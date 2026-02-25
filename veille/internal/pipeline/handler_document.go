// CLAUDE:SUMMARY Pipeline handler for document source type: local file extraction via docpipe.
package pipeline

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/hazyhaar/chrc/docpipe"
	"github.com/hazyhaar/chrc/extract"
	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/store"
)

// DocumentHandler handles local document files via docpipe.
// Source.URL is the local file path.
type DocumentHandler struct {
	pipe *docpipe.Pipeline
}

// NewDocumentHandler creates a DocumentHandler.
func NewDocumentHandler() *DocumentHandler {
	return &DocumentHandler{
		pipe: docpipe.New(docpipe.Config{}),
	}
}

// Handle extracts a local document, deduplicates by content hash, stores,
// and writes to buffer.
func (h *DocumentHandler) Handle(ctx context.Context, s *store.Store, src *store.Source, p *Pipeline) error {
	log := p.logger.With("source_id", src.ID, "path", src.URL, "handler", "document")
	start := time.Now()

	logEntry := &store.FetchLogEntry{
		ID:         p.newID(),
		SourceID:   src.ID,
		DurationMs: 0,
		FetchedAt:  time.Now().UnixMilli(),
	}

	// Extract the document.
	doc, err := h.pipe.Extract(ctx, src.URL)
	if err != nil {
		duration := time.Since(start).Milliseconds()
		logEntry.DurationMs = duration
		logEntry.Status = "extract_error"
		logEntry.ErrorMessage = err.Error()
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchError(ctx, src.ID, "docpipe: "+err.Error())
		log.Warn("document: extraction failed", "error", err)
		return fmt.Errorf("docpipe extract: %w", err)
	}

	text := extract.CleanText(doc.RawText)
	if text == "" {
		logEntry.Status = "empty"
		logEntry.DurationMs = time.Since(start).Milliseconds()
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchSuccess(ctx, src.ID, "")
		log.Debug("document: extracted text is empty")
		return nil
	}

	// Hash for dedup.
	h2 := sha256.Sum256([]byte(text))
	contentHash := fmt.Sprintf("%x", h2)

	// Dedup check.
	exists, err := s.ExtractionExists(ctx, src.ID, contentHash)
	if err != nil {
		return fmt.Errorf("document dedup: %w", err)
	}
	if exists {
		logEntry.Status = "unchanged"
		logEntry.ContentHash = contentHash
		logEntry.DurationMs = time.Since(start).Milliseconds()
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchUnchanged(ctx, src.ID)
		log.Debug("document: content unchanged")
		return nil
	}

	now := time.Now().UnixMilli()
	extractionID := p.newID()

	extraction := &store.Extraction{
		ID:            extractionID,
		SourceID:      src.ID,
		ContentHash:   contentHash,
		Title:         doc.Title,
		ExtractedText: text,
		URL:           src.URL,
		ExtractedAt:   now,
	}
	if err := s.InsertExtraction(ctx, extraction); err != nil {
		return fmt.Errorf("store extraction: %w", err)
	}

	// Write to buffer.
	if p.buffer != nil && p.currentJob != nil {
		meta := buffer.Metadata{
			ID:          extractionID,
			SourceID:    src.ID,
			DossierID:   p.currentJob.DossierID,
			SourceURL:   src.URL,
			SourceType:  "document",
			Title:       doc.Title,
			ContentHash: contentHash,
			ExtractedAt: time.Now().UTC(),
		}
		if _, err := p.buffer.Write(ctx, meta, text); err != nil {
			log.Warn("document: buffer write failed", "error", err)
		}
	}

	duration := time.Since(start).Milliseconds()
	logEntry.Status = "ok"
	logEntry.ContentHash = contentHash
	logEntry.DurationMs = duration
	s.InsertFetchLog(ctx, logEntry)
	s.RecordFetchSuccess(ctx, src.ID, contentHash)

	log.Info("document: processed",
		"title", doc.Title, "text_len", len(text), "duration_ms", duration)

	return nil
}
