package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"horos47/core/data"
)

// HandleDetectFormat detects file format and launches appropriate sub-pipeline.
func (h *Handlers) HandleDetectFormat(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("detect_format: %w", err)
	}

	files, err := extractFilesFromPreviousResult(payload)
	if err != nil || len(files) == 0 {
		h.Logger.Info("detect_format: no files to process", "envelope_id", envelopeID.String())
		resultJSON, _ := json.Marshal(map[string]string{"status": "no_files"})
		if err := h.GW.CompleteEnvelope(envelopeID, string(resultJSON)); err != nil {
			return nil, fmt.Errorf("complete envelope: %w", err)
		}
		return map[string]interface{}{"status": "no_files"}, nil
	}

	var submitted int
	for _, filePath := range files {
		format := detectFileFormat(filePath)
		h.Logger.Info("detect_format: file detected", "path", filePath, "format", format)

		switch format {
		case "pdf":
			if err := h.submitPDFPipeline(ctx, envelopeID, filePath); err != nil {
				h.Logger.Error("detect_format: PDF pipeline failed", "path", filePath, "error", err)
				continue
			}
			submitted++
		default:
			h.Logger.Warn("detect_format: unsupported format", "path", filePath, "format", format)
			h.createFailedTracker(envelopeID, filePath, format)
		}
	}

	if submitted == 0 {
		h.GW.FailEnvelope(envelopeID, "no supported file formats found")
	}

	return map[string]interface{}{
		"status":    "pipelines_launched",
		"submitted": submitted,
		"total":     len(files),
	}, nil
}

// HandleCompleteIngest checks if all ingest_trackers are done, then completes the envelope.
func (h *Handlers) HandleCompleteIngest(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("complete_ingest: %w", err)
	}

	var remaining int
	err = h.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ingest_trackers
		WHERE envelope_id = ? AND status NOT IN ('completed', 'failed')
	`, envelopeID).Scan(&remaining)
	if err != nil {
		return nil, fmt.Errorf("check trackers: %w", err)
	}

	if remaining > 0 {
		return map[string]interface{}{"status": "waiting", "remaining": remaining}, nil
	}

	var totalPages, completedPages, failedDocs int
	h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(total_pages), 0),
		       COALESCE(SUM(completed_pages), 0),
		       COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0)
		FROM ingest_trackers WHERE envelope_id = ?
	`, envelopeID).Scan(&totalPages, &completedPages, &failedDocs)

	result := map[string]interface{}{
		"status":           "ingestion_completed",
		"total_pages":      totalPages,
		"completed_pages":  completedPages,
		"failed_documents": failedDocs,
	}
	resultJSON, _ := json.Marshal(result)

	if err := h.GW.CompleteEnvelope(envelopeID, string(resultJSON)); err != nil {
		return nil, fmt.Errorf("complete envelope: %w", err)
	}

	if err := h.GW.DispatchResult(ctx, envelopeID); err != nil {
		h.Logger.Error("complete_ingest: dispatch failed (non-fatal)", "error", err)
	}

	h.Logger.Info("complete_ingest: envelope completed", "envelope_id", envelopeID.String(),
		"total_pages", totalPages, "completed_pages", completedPages)

	return result, nil
}

// --- Helpers ---

func (h *Handlers) submitPDFPipeline(ctx context.Context, envelopeID data.UUID, filePath string) error {
	totalPages, err := countPDFPages(filePath)
	if err != nil {
		return fmt.Errorf("count pages: %w", err)
	}

	docID := data.NewUUID()
	filename := filepath.Base(filePath)
	_, err = data.ExecWithRetry(h.DB, `
		INSERT INTO documents (document_id, title, source, content_type, metadata, created_at)
		VALUES (?, ?, 'gateway_ingest', 'application/pdf', '{}', unixepoch())
	`, docID, filename)
	if err != nil {
		return fmt.Errorf("create document: %w", err)
	}

	trackerID := data.NewUUID()
	_, err = data.ExecWithRetry(h.DB, `
		INSERT INTO ingest_trackers
			(tracker_id, envelope_id, document_id, file_path, file_format, total_pages, status, created_at)
		VALUES (?, ?, ?, ?, 'pdf', ?, 'processing', unixepoch())
	`, trackerID, envelopeID, docID, filePath, totalPages)
	if err != nil {
		return fmt.Errorf("create tracker: %w", err)
	}

	fileHash, _ := computeFileHash(filePath)
	runID := data.NewUUID()
	_, err = h.Queue.Submit("pdf_to_images", map[string]interface{}{
		"pdf_path":       filePath,
		"envelope_id":    envelopeID.String(),
		"document_id":    docID.String(),
		"file_hash":      fileHash,
		"resolution_dpi": 300,
		"_workflow": map[string]interface{}{
			"chain":  []string{"image_to_ocr", "ocr_to_database"},
			"run_id": runID.String(),
		},
	})
	if err != nil {
		return fmt.Errorf("submit pdf_to_images: %w", err)
	}

	h.Logger.Info("detect_format: PDF pipeline submitted",
		"envelope_id", envelopeID.String(),
		"document_id", docID.String(),
		"pages", totalPages)
	return nil
}

func (h *Handlers) createFailedTracker(envelopeID data.UUID, filePath, format string) {
	trackerID := data.NewUUID()
	docID := data.NewUUID()
	data.ExecWithRetry(h.DB, `
		INSERT INTO ingest_trackers
			(tracker_id, envelope_id, document_id, file_path, file_format, total_pages, status, created_at)
		VALUES (?, ?, ?, ?, ?, 0, 'failed', unixepoch())
	`, trackerID, envelopeID, docID, filePath, format)
}

func extractFilesFromPreviousResult(payload map[string]interface{}) ([]string, error) {
	prevResultStr, ok := payload["previous_result"].(string)
	if !ok {
		return nil, fmt.Errorf("no previous_result in payload")
	}
	var prevResult map[string]interface{}
	if err := json.Unmarshal([]byte(prevResultStr), &prevResult); err != nil {
		return nil, fmt.Errorf("parse previous_result: %w", err)
	}
	filesRaw, ok := prevResult["files"]
	if !ok {
		return nil, nil
	}
	filesArr, ok := filesRaw.([]interface{})
	if !ok {
		return nil, nil
	}
	var files []string
	for _, f := range filesArr {
		if s, ok := f.(string); ok {
			files = append(files, s)
		}
	}
	return files, nil
}

func detectFileFormat(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return "unknown"
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "unknown"
	}

	mime := http.DetectContentType(buf[:n])
	switch {
	case mime == "application/pdf":
		return "pdf"
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case mime == "text/plain; charset=utf-8", mime == "text/plain":
		return "text"
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".tiff":
		return "image"
	case ".txt", ".md", ".csv":
		return "text"
	default:
		return "unknown"
	}
}
