package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"horos47/core/data"
	"horos47/core/jobs"
	workflow_trace "horos47/core/trace"
)

// HandleImageToOCR performs OCR on an image via the GPU Feeder.
func (h *Handlers) HandleImageToOCR(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	h.Logger.Info("Handler Image → OCR starting")

	imagePath, ok := payload["image_path"].(string)
	if !ok {
		return nil, fmt.Errorf("missing image_path in payload")
	}
	pageNum := int(payload["page_number"].(float64))
	docID := payload["document_id"].(string)

	var workflowRunID string
	if wfData, ok := payload["_workflow"].(map[string]interface{}); ok {
		if runID, ok := wfData["run_id"].(string); ok {
			workflowRunID = runID
		}
	}
	if workflowRunID == "" {
		workflowRunID = data.NewUUID().String()
	}

	inputHash, err := computeFileHash(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to hash image: %w", err)
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
		h.Logger.Info("Image already processed, skipping OCR", "trace_id", existingTraceID, "page", pageNum)
		return h.loadExistingOCRAndContinue(ctx, existingTraceID, payload, docID, pageNum, workflowRunID)
	}

	workDir := filepath.Dir(filepath.Dir(imagePath))
	ocrDir := filepath.Join(workDir, "image_to_ocr")
	if err := os.MkdirAll(ocrDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create ocr dir: %w", err)
	}
	ocrPath := filepath.Join(ocrDir, fmt.Sprintf("page-%03d.json", pageNum))

	metadata := map[string]interface{}{
		"page_number": pageNum,
		"document_id": docID,
	}
	traceID, _ := tracer.TraceStepStart("image_to_ocr", pageNum-1, imagePath, metadata)

	if h.GPUSubmitter == nil {
		if traceID != "" {
			tracer.TraceStepFailed(traceID, "GPU_SUBMITTER_UNAVAILABLE", "GPU Submitter not initialized")
		}
		return nil, fmt.Errorf("GPU Submitter not initialized")
	}

	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		if traceID != "" {
			tracer.TraceStepFailed(traceID, "IMAGE_READ_ERROR", err.Error())
		}
		return nil, fmt.Errorf("failed to read image: %w", err)
	}

	imageBase64 := base64.StdEncoding.EncodeToString(imageData)
	format := "png"
	if strings.HasSuffix(strings.ToLower(imagePath), ".jpg") || strings.HasSuffix(strings.ToLower(imagePath), ".jpeg") {
		format = "jpeg"
	}

	ocrResp, err := h.GPUSubmitter.SubmitVision(ctx, imageBase64, format)
	if err != nil {
		if traceID != "" {
			tracer.TraceStepFailed(traceID, "OCR_ERROR", err.Error())
		}
		return nil, fmt.Errorf("OCR failed via GPU Submitter: %w", err)
	}

	ocrData := map[string]interface{}{
		"page":       pageNum,
		"text":       ocrResp.Text,
		"model":      ocrResp.Model,
		"timestamp":  time.Now().Unix(),
		"image_path": imagePath,
	}
	jsonBytes, _ := json.MarshalIndent(ocrData, "", "  ")
	if err := os.WriteFile(ocrPath, jsonBytes, 0644); err != nil {
		if traceID != "" {
			tracer.TraceStepFailed(traceID, "FILE_WRITE_ERROR", err.Error())
		}
		return nil, fmt.Errorf("failed to write OCR JSON: %w", err)
	}

	h.Logger.Info("OCR saved", "page", pageNum, "tokens", ocrResp.TokensUsed)

	if traceID != "" {
		completeMetadata := map[string]interface{}{
			"tokens_used": ocrResp.TokensUsed,
			"text_length": len(ocrResp.Text),
			"model":       ocrResp.Model,
		}
		tracer.TraceStepComplete(traceID, ocrPath, []string{ocrPath}, completeMetadata)
	}

	return h.createNextOCRJob(payload, ocrPath, docID, pageNum, workflowRunID)
}

func (h *Handlers) loadExistingOCRAndContinue(ctx context.Context, traceID string, payload map[string]interface{}, docID string, pageNum int, workflowRunID string) (map[string]interface{}, error) {
	var ocrPath string
	err := h.DB.QueryRowContext(ctx, `
		SELECT output_file_path FROM workflow_execution_trace WHERE trace_id = ?
	`, traceID).Scan(&ocrPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load existing trace: %w", err)
	}
	return h.createNextOCRJob(payload, ocrPath, docID, pageNum, workflowRunID)
}

func (h *Handlers) createNextOCRJob(payload map[string]interface{}, ocrPath, docID string, pageNum int, workflowRunID string) (map[string]interface{}, error) {
	workflowData, ok := payload["_workflow"].(map[string]interface{})
	if !ok {
		return map[string]interface{}{"ocr_path": ocrPath}, nil
	}
	chainRaw, ok := workflowData["chain"].([]interface{})
	if !ok || len(chainRaw) == 0 {
		return map[string]interface{}{"ocr_path": ocrPath}, nil
	}

	nextJobType := chainRaw[0].(string)
	var remainingChain []interface{}
	if len(chainRaw) > 1 {
		remainingChain = chainRaw[1:]
	}

	queue, _ := jobs.NewQueue(h.DB)
	childPayload := map[string]interface{}{
		"ocr_json_path": ocrPath,
		"document_id":   docID,
		"page_number":   float64(pageNum),
		"_workflow": map[string]interface{}{
			"chain":  remainingChain,
			"run_id": workflowRunID,
		},
	}
	if eid, ok := payload["envelope_id"]; ok {
		childPayload["envelope_id"] = eid
	}
	queue.Submit(nextJobType, childPayload)

	return map[string]interface{}{
		"ocr_path":        ocrPath,
		"next_job":        nextJobType,
		"page_number":     pageNum,
		"workflow_run_id": workflowRunID,
	}, nil
}
