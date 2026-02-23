package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"horos47/core/data"
	"horos47/core/jobs"
	workflow_trace "horos47/core/trace"
	"horos47/storage"
)

// HandleOCRToDatabase inserts OCR JSON into the database with blob splitting.
func (h *Handlers) HandleOCRToDatabase(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	h.Logger.Info("Handler OCR → Database starting")

	ocrPath, ok := payload["ocr_json_path"].(string)
	if !ok {
		return nil, fmt.Errorf("missing ocr_json_path in payload")
	}
	pageNum := int(payload["page_number"].(float64))
	docIDStr := payload["document_id"].(string)
	docID, err := data.ParseUUID(docIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid document_id: %w", err)
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

	inputHash, err := computeFileHash(ocrPath)
	if err != nil {
		return nil, fmt.Errorf("failed to hash OCR JSON: %w", err)
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
		h.Logger.Info("OCR already inserted, skipping", "trace_id", existingTraceID, "page", pageNum)
		return h.createNextDatabaseJob(ctx, payload, docID, pageNum, workflowRunID)
	}

	// Read OCR JSON
	jsonData, err := os.ReadFile(ocrPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read OCR JSON: %w", err)
	}
	var ocrData map[string]interface{}
	if err := json.Unmarshal(jsonData, &ocrData); err != nil {
		return nil, fmt.Errorf("failed to parse OCR JSON: %w", err)
	}

	ocrText, _ := ocrData["text"].(string)
	confidence, _ := ocrData["confidence"].(float64)
	imagePath, _ := ocrData["image_path"].(string)

	traceMetadata := map[string]interface{}{
		"page_number": pageNum,
		"document_id": docIDStr,
		"confidence":  confidence,
	}
	traceID, _ := tracer.TraceStepStart("ocr_to_database", pageNum-1, ocrPath, traceMetadata)

	// Read image for blob storage
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		if traceID != "" {
			tracer.TraceStepFailed(traceID, "IMAGE_READ_ERROR", err.Error())
		}
		return nil, fmt.Errorf("failed to read image: %w", err)
	}

	hashBytes := sha256.Sum256(imageData)
	imageHash := fmt.Sprintf("%x", hashBytes)
	blobs := storage.SplitImageBlobs(imageData)

	// Build PDFPages
	var pdfPages []storage.PDFPage
	now := time.Now()
	pageMetadata := map[string]interface{}{
		"confidence": confidence,
		"model":      ocrData["model"],
		"timestamp":  ocrData["timestamp"],
	}

	for blobIdx, blobData := range blobs {
		page := storage.PDFPage{
			PageID:     data.NewUUID(),
			DocumentID: docID,
			PageNumber: pageNum,
			BlobIndex:  blobIdx,
			TotalBlobs: len(blobs),
			BlobData:   blobData,
			ImageHash:  imageHash,
			Metadata:   pageMetadata,
			CreatedAt:  now,
		}
		if blobIdx == 0 {
			page.OCRText = ocrText
		}
		pdfPages = append(pdfPages, page)
	}

	if err := storage.SavePDFPages(h.DB, pdfPages); err != nil {
		if traceID != "" {
			tracer.TraceStepFailed(traceID, "DATABASE_INSERT_ERROR", err.Error())
		}
		return nil, fmt.Errorf("failed to save PDF pages: %w", err)
	}

	// Create chunks for RAG
	var chunkCount int
	if len(ocrText) > 0 {
		chunkCount, err = h.createChunksFromOCR(ctx, docID, pageNum, ocrText)
		if err != nil {
			h.Logger.Warn("Failed to create chunks", "page", pageNum, "error", err)
		}
	}

	// Write metadata output
	workDir := filepath.Dir(filepath.Dir(ocrPath))
	dbDir := filepath.Join(workDir, "ocr_to_database")
	os.MkdirAll(dbDir, 0755)
	outputPath := filepath.Join(dbDir, fmt.Sprintf("page-%03d-metadata.json", pageNum))
	outputData := map[string]interface{}{
		"page_number": pageNum,
		"document_id": docIDStr,
		"blobs_count": len(blobs),
		"chunk_count": chunkCount,
		"ocr_length":  len(ocrText),
		"timestamp":   time.Now().Unix(),
	}
	outputJSON, _ := json.MarshalIndent(outputData, "", "  ")
	os.WriteFile(outputPath, outputJSON, 0644)

	if traceID != "" {
		completeMetadata := map[string]interface{}{
			"blobs_count": len(blobs),
			"chunk_count": chunkCount,
			"ocr_length":  len(ocrText),
		}
		tracer.TraceStepComplete(traceID, outputPath, []string{outputPath}, completeMetadata)
	}

	// Update ingest tracker for gateway-initiated flows
	if envelopeIDStr, ok := payload["envelope_id"].(string); ok && envelopeIDStr != "" {
		h.updateIngestTracker(ctx, docID, envelopeIDStr)
	}

	return h.createNextDatabaseJob(ctx, payload, docID, pageNum, workflowRunID)
}

func (h *Handlers) createChunksFromOCR(ctx context.Context, docID data.UUID, pageNum int, ocrText string) (int, error) {
	textChunks := storage.ChunkBySentences(ocrText, 200)
	if len(textChunks) == 0 {
		return 0, nil
	}

	err := data.RunTransaction(h.DB, func(tx *sql.Tx) error {
		for i, chunkText := range textChunks {
			chunk := storage.Chunk{
				ID:         data.NewUUID(),
				DocumentID: docID,
				ChunkIndex: i,
				ChunkText:  chunkText,
				WordCount:  len(strings.Fields(chunkText)),
				CreatedAt:  time.Now(),
			}
			_, err := tx.ExecContext(ctx, `
				INSERT INTO chunks (chunk_id, document_id, chunk_index, chunk_text, word_count, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, chunk.ID, chunk.DocumentID, chunk.ChunkIndex, chunk.ChunkText, chunk.WordCount, chunk.CreatedAt.Unix())
			if err != nil {
				return fmt.Errorf("insert chunk %d: %w", i, err)
			}
			// Sync FTS5 index
			_, err = tx.ExecContext(ctx, `
				INSERT INTO chunks_fts (chunk_id, chunk_text) VALUES (?, ?)
			`, chunk.ID, chunk.ChunkText)
			if err != nil {
				return fmt.Errorf("insert chunk_fts %d: %w", i, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(textChunks), nil
}

func (h *Handlers) createNextDatabaseJob(ctx context.Context, payload map[string]interface{}, docID data.UUID, pageNum int, workflowRunID string) (map[string]interface{}, error) {
	workflowData, ok := payload["_workflow"].(map[string]interface{})
	if !ok {
		return map[string]interface{}{"page_saved": pageNum, "document_id": docID.String(), "workflow_run_id": workflowRunID}, nil
	}
	chainRaw, ok := workflowData["chain"].([]interface{})
	if !ok || len(chainRaw) == 0 {
		return map[string]interface{}{"page_saved": pageNum, "document_id": docID.String(), "workflow_run_id": workflowRunID}, nil
	}

	nextJobType := chainRaw[0].(string)
	var remainingChain []interface{}
	if len(chainRaw) > 1 {
		remainingChain = chainRaw[1:]
	}

	queue, _ := jobs.NewQueue(h.DB)
	queue.Submit(nextJobType, map[string]interface{}{
		"document_id": docID.String(),
		"page_number": float64(pageNum),
		"_workflow": map[string]interface{}{
			"chain":  remainingChain,
			"run_id": workflowRunID,
		},
	})

	return map[string]interface{}{
		"page_saved":      pageNum,
		"document_id":     docID.String(),
		"next_job":        nextJobType,
		"workflow_run_id": workflowRunID,
	}, nil
}

func (h *Handlers) updateIngestTracker(ctx context.Context, docID data.UUID, envelopeIDStr string) {
	var completedPages, totalPages int
	err := h.DB.QueryRowContext(ctx, `
		UPDATE ingest_trackers
		SET completed_pages = completed_pages + 1,
		    status = CASE WHEN completed_pages + 1 >= total_pages THEN 'completed' ELSE status END,
		    completed_at = CASE WHEN completed_pages + 1 >= total_pages THEN unixepoch() ELSE completed_at END
		WHERE document_id = ?
		RETURNING completed_pages, total_pages
	`, docID).Scan(&completedPages, &totalPages)
	if err != nil {
		h.Logger.Warn("Failed to update ingest tracker", "document_id", docID.String(), "error", err)
		return
	}

	if completedPages < totalPages {
		return
	}

	envelopeID, err := data.ParseUUID(envelopeIDStr)
	if err != nil {
		return
	}

	var remaining int
	h.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ingest_trackers
		WHERE envelope_id = ? AND status NOT IN ('completed', 'failed')
	`, envelopeID).Scan(&remaining)

	if remaining > 0 {
		return
	}

	queue, _ := jobs.NewQueue(h.DB)
	queue.Submit("complete_ingest", map[string]interface{}{
		"envelope_id": envelopeIDStr,
	})

	h.Logger.Info("All documents complete, complete_ingest job submitted", "envelope_id", envelopeIDStr)
}
