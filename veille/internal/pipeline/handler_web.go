// CLAUDE:SUMMARY Pipeline handler for web source type: HTTP fetch, HTML extract, dedup, store.
package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/hazyhaar/chrc/extract"
	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/store"
)

// WebHandler handles web (HTTP GET) sources.
type WebHandler struct{}

// Handle fetches a URL, extracts content, and stores the extraction.
func (h *WebHandler) Handle(ctx context.Context, s *store.Store, src *store.Source, p *Pipeline) error {
	log := p.logger.With("source_id", src.ID, "url", src.URL, "handler", "web")
	start := time.Now()

	// Fetch with conditional GET.
	result, err := p.fetcher.Fetch(ctx, src.URL, "", "", src.LastHash)
	duration := time.Since(start).Milliseconds()

	logEntry := &store.FetchLogEntry{
		ID:         p.newID(),
		SourceID:   src.ID,
		DurationMs: duration,
		FetchedAt:  time.Now().UnixMilli(),
	}

	if err != nil {
		logEntry.Status = "error"
		logEntry.ErrorMessage = err.Error()
		if result != nil {
			logEntry.StatusCode = result.StatusCode
		}
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchError(ctx, src.ID, err.Error())
		log.Warn("web: fetch failed", "error", err, "duration_ms", duration)
		return fmt.Errorf("fetch: %w", err)
	}

	logEntry.StatusCode = result.StatusCode
	logEntry.ContentHash = result.Hash

	if !result.Changed {
		logEntry.Status = "unchanged"
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchUnchanged(ctx, src.ID)
		log.Debug("web: content unchanged", "duration_ms", duration)
		return nil
	}

	// Extract content.
	extractResult, err := extract.Extract(result.Body, extract.Options{Mode: "auto"})
	if err != nil {
		logEntry.Status = "extract_error"
		logEntry.ErrorMessage = err.Error()
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchError(ctx, src.ID, "extract: "+err.Error())
		log.Warn("web: extraction failed", "error", err)
		return fmt.Errorf("extract: %w", err)
	}

	cleanText := extract.CleanText(extractResult.Text)
	if cleanText == "" {
		logEntry.Status = "empty"
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchSuccess(ctx, src.ID, result.Hash)
		log.Debug("web: extracted text is empty")
		return nil
	}

	now := time.Now().UnixMilli()
	extractionID := p.newID()

	// Store extraction (FTS5 trigger handles indexing).
	extraction := &store.Extraction{
		ID:            extractionID,
		SourceID:      src.ID,
		ContentHash:   extractResult.Hash,
		Title:         extractResult.Title,
		ExtractedText: cleanText,
		ExtractedHTML: extractResult.HTML,
		URL:           src.URL,
		ExtractedAt:   now,
	}
	if err := s.InsertExtraction(ctx, extraction); err != nil {
		return fmt.Errorf("store extraction: %w", err)
	}

	// Write to buffer if configured.
	if p.buffer != nil {
		meta := buffer.Metadata{
			ID:          extractionID,
			SourceID:    src.ID,
			DossierID:   p.currentJob.DossierID,
			SourceURL:   src.URL,
			SourceType:  src.SourceType,
			Title:       extractResult.Title,
			ContentHash: extractResult.Hash,
			ExtractedAt: time.Now().UTC(),
		}
		if _, err := p.buffer.Write(ctx, meta, cleanText); err != nil {
			log.Warn("web: buffer write failed", "error", err)
		}
	}

	logEntry.Status = "ok"
	s.InsertFetchLog(ctx, logEntry)
	s.RecordFetchSuccess(ctx, src.ID, result.Hash)

	log.Info("web: processed", "text_len", len(cleanText), "duration_ms", duration)

	return nil
}
