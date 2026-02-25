// CLAUDE:SUMMARY Pipeline handler for API source type: fetches JSON, walks results, dedup, extract, store.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hazyhaar/chrc/extract"
	"github.com/hazyhaar/chrc/veille/internal/apifetch"
	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/store"
)

// APIHandler handles JSON API sources using the apifetch package.
type APIHandler struct {
	client *http.Client
}

// NewAPIHandler creates an APIHandler with a default HTTP client.
func NewAPIHandler() *APIHandler {
	return &APIHandler{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Handle fetches from a JSON API, deduplicates per result, stores and buffers.
func (h *APIHandler) Handle(ctx context.Context, s *store.Store, src *store.Source, p *Pipeline) error {
	log := p.logger.With("source_id", src.ID, "url", src.URL, "handler", "api")
	start := time.Now()

	// Parse API config from source.config_json.
	var cfg apifetch.Config
	if src.ConfigJSON != "" && src.ConfigJSON != "{}" {
		if err := json.Unmarshal([]byte(src.ConfigJSON), &cfg); err != nil {
			return fmt.Errorf("api config: %w", err)
		}
	}

	logEntry := &store.FetchLogEntry{
		ID:         p.newID(),
		SourceID:   src.ID,
		DurationMs: 0,
		FetchedAt:  time.Now().UnixMilli(),
	}

	// Fetch from API.
	results, err := apifetch.Fetch(ctx, h.client, src.URL, cfg)
	duration := time.Since(start).Milliseconds()
	logEntry.DurationMs = duration

	if err != nil {
		logEntry.Status = "error"
		logEntry.ErrorMessage = err.Error()
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchError(ctx, src.ID, err.Error())
		log.Warn("api: fetch failed", "error", err)
		return fmt.Errorf("api fetch: %w", err)
	}

	// Process each result.
	var newCount int
	for _, r := range results {
		text := extract.CleanText(r.Text)
		if text == "" {
			continue
		}

		contentHash := hashString(r.URL + "|" + r.Title)

		// Dedup check.
		exists, err := s.ExtractionExists(ctx, src.ID, contentHash)
		if err != nil {
			log.Warn("api: dedup check failed", "error", err)
			continue
		}
		if exists {
			continue
		}

		now := time.Now().UnixMilli()
		extractionID := p.newID()

		url := r.URL
		if url == "" {
			url = src.URL
		}

		extraction := &store.Extraction{
			ID:            extractionID,
			SourceID:      src.ID,
			ContentHash:   contentHash,
			Title:         r.Title,
			ExtractedText: text,
			URL:           url,
			ExtractedAt:   now,
		}
		if err := s.InsertExtraction(ctx, extraction); err != nil {
			log.Warn("api: insert extraction failed", "error", err)
			continue
		}

		// Write to buffer.
		if p.buffer != nil && p.currentJob != nil {
			meta := buffer.Metadata{
				ID:          extractionID,
				SourceID:    src.ID,
				DossierID:   p.currentJob.DossierID,
				SourceURL:   url,
				SourceType:  "api",
				Title:       r.Title,
				ContentHash: contentHash,
				ExtractedAt: time.Now().UTC(),
			}
			if _, err := p.buffer.Write(ctx, meta, text); err != nil {
				log.Warn("api: buffer write failed", "error", err)
			}
		}

		newCount++
	}

	logEntry.Status = "ok"
	s.InsertFetchLog(ctx, logEntry)
	s.RecordFetchSuccess(ctx, src.ID, "")

	log.Info("api: processed", "results", len(results), "new", newCount, "duration_ms", duration)

	return nil
}
