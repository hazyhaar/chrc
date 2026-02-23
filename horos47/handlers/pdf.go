package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"horos47/core/data"
	"horos47/core/jobs"
	workflow_trace "horos47/core/trace"
)

// HandlePDFToImages converts PDF to PNG images using pdftoppm (parallel chunked).
func (h *Handlers) HandlePDFToImages(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	h.Logger.Info("Handler PDF → Images starting (Parallel Chunked)")

	pdfPath, ok := payload["pdf_path"].(string)
	if !ok {
		return nil, fmt.Errorf("missing pdf_path in payload")
	}

	resolution := 150 // Default Gemini (adaptive resolution managed by GPU Feeder V3 allocator)
	if res, ok := payload["resolution_dpi"].(float64); ok {
		resolution = int(res)
	}

	// SHA256 for idempotence
	inputHash, err := computeFileHash(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to hash PDF: %w", err)
	}

	var workflowRunID string
	if wfData, ok := payload["_workflow"].(map[string]interface{}); ok {
		if runID, ok := wfData["run_id"].(string); ok {
			workflowRunID = runID
		}
	}
	if workflowRunID == "" {
		workflowRunID = data.NewUUID().String()
	}

	machineName, _ := os.Hostname()
	tracer, err := workflow_trace.NewWorkflowTracer(h.DB, "vision_pdf_ocr", workflowRunID, machineName)
	if err != nil {
		return nil, fmt.Errorf("failed to create tracer: %w", err)
	}
	defer tracer.Close()

	isDuplicate, existingTraceID, err := tracer.CheckDuplicate(inputHash)
	if err != nil {
		h.Logger.Warn("CheckDuplicate failed, continuing", "error", err)
	}
	if isDuplicate {
		h.Logger.Info("PDF already processed, skipping", "trace_id", existingTraceID)
		return h.loadExistingPDFResults(ctx, existingTraceID, payload, workflowRunID)
	}

	docID := data.NewUUID()
	workDir := filepath.Join("/inference/agents/sources/processing", docID.String())
	imageDir := filepath.Join(workDir, "pdf_to_images")
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work dir: %w", err)
	}

	metadata := map[string]interface{}{
		"resolution_dpi": resolution,
		"document_id":    docID.String(),
		"strategy":       "parallel_chunked",
	}
	traceID, _ := tracer.TraceStepStart("pdf_to_images", 0, pdfPath, metadata)

	// Count pages
	totalPages, err := countPDFPages(pdfPath)
	if err != nil {
		if traceID != "" {
			tracer.TraceStepFailed(traceID, "PDFINFO_ERROR", err.Error())
		}
		return nil, fmt.Errorf("pdfinfo failed: %w", err)
	}

	const chunkSize = 10
	const maxConcurrent = 4

	h.Logger.Info("Starting parallel conversion", "pages", totalPages, "chunk_size", chunkSize)

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrent)
	errChan := make(chan error, totalPages/chunkSize+1)

	queue, err := jobs.NewQueue(h.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to create queue: %w", err)
	}

	var nextJobType string
	var remainingChain []interface{}
	if wfData, ok := payload["_workflow"].(map[string]interface{}); ok {
		if chain, ok := wfData["chain"].([]interface{}); ok && len(chain) > 0 {
			nextJobType = chain[0].(string)
			if len(chain) > 1 {
				remainingChain = chain[1:]
			}
		}
	}

	totalGeneratedImages := 0
	var processedMu sync.Mutex

	for startPage := 1; startPage <= totalPages; startPage += chunkSize {
		endPage := startPage + chunkSize - 1
		if endPage > totalPages {
			endPage = totalPages
		}

		wg.Add(1)
		semaphore <- struct{}{}

		go func(start, end int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			outputPrefix := filepath.Join(imageDir, "page")
			cmd := exec.Command("pdftoppm",
				"-f", fmt.Sprintf("%d", start),
				"-l", fmt.Sprintf("%d", end),
				"-png",
				"-r", fmt.Sprintf("%d", resolution),
				pdfPath,
				outputPrefix)

			if err := cmd.Run(); err != nil {
				errChan <- fmt.Errorf("chunk %d-%d failed: %w", start, end, err)
				return
			}

			entries, err := os.ReadDir(imageDir)
			if err != nil {
				h.Logger.Error("Failed to list image dir", "error", err)
				return
			}

			foundCount := 0
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasPrefix(entry.Name(), "page-") || !strings.HasSuffix(entry.Name(), ".png") {
					continue
				}
				namePart := strings.TrimPrefix(entry.Name(), "page-")
				namePart = strings.TrimSuffix(namePart, ".png")
				var num int
				if _, err := fmt.Sscanf(namePart, "%d", &num); err != nil {
					continue
				}
				if num >= start && num <= end {
					imagePath := filepath.Join(imageDir, entry.Name())
					if nextJobType != "" {
						childPayload := map[string]interface{}{
							"image_path":  imagePath,
							"page_number": float64(num),
							"document_id": docID.String(),
							"_workflow": map[string]interface{}{
								"chain":  remainingChain,
								"run_id": workflowRunID,
							},
						}
						if eid, ok := payload["envelope_id"]; ok {
							childPayload["envelope_id"] = eid
						}
						if _, err := queue.Submit(nextJobType, childPayload); err == nil {
							processedMu.Lock()
							totalGeneratedImages++
							processedMu.Unlock()
							foundCount++
						}
					}
				}
			}
			h.Logger.Info("Chunk processed", "start", start, "end", end, "images_found", foundCount)
		}(startPage, endPage)
	}

	wg.Wait()
	close(errChan)

	var failure error
	for err := range errChan {
		h.Logger.Error("Chunk conversion error", "error", err)
		failure = err
	}
	if failure != nil {
		if traceID != "" {
			tracer.TraceStepFailed(traceID, "CHUNK_ERROR", failure.Error())
		}
		return nil, fmt.Errorf("conversion finished with errors: %w", failure)
	}

	if traceID != "" {
		finalImages, _ := filepath.Glob(filepath.Join(imageDir, "page-*.png"))
		outputPath := filepath.Join(imageDir, "manifest.txt")
		manifestFile, _ := os.Create(outputPath)
		for _, img := range finalImages {
			fmt.Fprintln(manifestFile, img)
		}
		manifestFile.Close()
		completeMetadata := map[string]interface{}{
			"images_count": len(finalImages),
			"total_pages":  totalPages,
		}
		tracer.TraceStepComplete(traceID, outputPath, finalImages, completeMetadata)
	}

	return map[string]interface{}{
		"images_count":    totalGeneratedImages,
		"document_id":     docID.String(),
		"work_dir":        workDir,
		"workflow_run_id": workflowRunID,
	}, nil
}

func (h *Handlers) loadExistingPDFResults(ctx context.Context, traceID string, payload map[string]interface{}, workflowRunID string) (map[string]interface{}, error) {
	var artifactPathsJSON string
	var documentIDStr string
	err := h.DB.QueryRowContext(ctx, `
		SELECT artifact_paths, json_extract(step_metadata, '$.document_id')
		FROM workflow_execution_trace WHERE trace_id = ?
	`, traceID).Scan(&artifactPathsJSON, &documentIDStr)
	if err != nil {
		return nil, fmt.Errorf("failed to load existing trace: %w", err)
	}

	var images []string
	if err := json.Unmarshal([]byte(artifactPathsJSON), &images); err != nil {
		return nil, fmt.Errorf("failed to parse artifact paths: %w", err)
	}

	docID, _ := data.ParseUUID(documentIDStr)
	return h.createNextJobsFromImages(payload, images, docID, workflowRunID)
}

func (h *Handlers) createNextJobsFromImages(payload map[string]interface{}, images []string, docID data.UUID, workflowRunID string) (map[string]interface{}, error) {
	workflowData, ok := payload["_workflow"].(map[string]interface{})
	if !ok {
		return map[string]interface{}{"images_count": len(images), "document_id": docID.String()}, nil
	}

	chainRaw, ok := workflowData["chain"].([]interface{})
	if !ok || len(chainRaw) == 0 {
		return map[string]interface{}{"images_count": len(images), "document_id": docID.String()}, nil
	}

	nextJobType := chainRaw[0].(string)
	var remainingChain []interface{}
	if len(chainRaw) > 1 {
		remainingChain = chainRaw[1:]
	}

	queue, _ := jobs.NewQueue(h.DB)
	for _, imagePath := range images {
		basename := filepath.Base(imagePath)
		var pageNum int
		fmt.Sscanf(basename, "page-%d.png", &pageNum)

		childPayload := map[string]interface{}{
			"image_path":  imagePath,
			"page_number": float64(pageNum),
			"document_id": docID.String(),
			"_workflow": map[string]interface{}{
				"chain":  remainingChain,
				"run_id": workflowRunID,
			},
		}
		if eid, ok := payload["envelope_id"]; ok {
			childPayload["envelope_id"] = eid
		}
		queue.Submit(nextJobType, childPayload)
	}

	return map[string]interface{}{
		"images_count":    len(images),
		"document_id":     docID.String(),
		"workflow_run_id": workflowRunID,
	}, nil
}

// --- Helpers ---

func computeFileHash(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func countPDFPages(pdfPath string) (int, error) {
	cmd := exec.Command("pdfinfo", pdfPath)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("pdfinfo failed: %w", err)
	}
	var pages int
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Pages:") {
			fmt.Sscanf(line, "Pages: %d", &pages)
			break
		}
	}
	if pages == 0 {
		return 0, fmt.Errorf("could not determine page count")
	}
	return pages, nil
}
