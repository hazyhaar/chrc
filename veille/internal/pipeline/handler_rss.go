// CLAUDE:SUMMARY Pipeline handler for RSS source type: parses feed, per-entry dedup, extract, store.
package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hazyhaar/chrc/extract"
	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/feed"
	"github.com/hazyhaar/chrc/veille/internal/store"
)

// RSSConfig is parsed from source.config_json for RSS sources.
type RSSConfig struct {
	MaxEntries  int  `json:"max_entries"`
	FollowLinks bool `json:"follow_links"`
}

// RSSHandler handles RSS/Atom feed sources.
type RSSHandler struct{}

// Handle fetches the feed, parses entries, deduplicates, and stores extractions.
func (h *RSSHandler) Handle(ctx context.Context, s *store.Store, src *store.Source, p *Pipeline) error {
	log := p.logger.With("source_id", src.ID, "url", src.URL, "handler", "rss")
	start := time.Now()

	// Parse config.
	cfg := RSSConfig{MaxEntries: 50}
	if src.ConfigJSON != "" && src.ConfigJSON != "{}" {
		json.Unmarshal([]byte(src.ConfigJSON), &cfg)
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 50
	}

	// Fetch the feed XML.
	result, err := p.fetcher.Fetch(ctx, src.URL, "", "", "")
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
		log.Warn("rss: fetch failed", "error", err)
		return fmt.Errorf("rss fetch: %w", err)
	}

	logEntry.StatusCode = result.StatusCode
	logEntry.ContentHash = result.Hash

	// Parse the feed.
	f, err := feed.Parse(result.Body)
	if err != nil {
		logEntry.Status = "extract_error"
		logEntry.ErrorMessage = err.Error()
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchError(ctx, src.ID, "parse: "+err.Error())
		log.Warn("rss: parse failed", "error", err)
		return fmt.Errorf("rss parse: %w", err)
	}

	// Process entries.
	var newCount int
	limit := cfg.MaxEntries
	if limit > len(f.Entries) {
		limit = len(f.Entries)
	}

	for _, entry := range f.Entries[:limit] {
		// Build content hash from GUID or Link for dedup.
		hashInput := entry.GUID
		if hashInput == "" {
			hashInput = entry.Link
		}
		contentHash := hashString(hashInput)

		// Dedup check.
		exists, err := s.ExtractionExists(ctx, src.ID, contentHash)
		if err != nil {
			log.Warn("rss: dedup check failed", "error", err)
			continue
		}
		if exists {
			continue
		}

		// Get text content: prefer full content, fallback to description.
		text := entry.Content
		if text == "" {
			text = entry.Description
		}

		// If follow_links and we have a link, fetch and extract the full page.
		if cfg.FollowLinks && entry.Link != "" {
			pageResult, fetchErr := p.fetcher.Fetch(ctx, entry.Link, "", "", "")
			if fetchErr == nil && pageResult.Changed {
				extractResult, extractErr := extract.Extract(pageResult.Body, extract.Options{Mode: "auto"})
				if extractErr == nil && extractResult.Text != "" {
					text = extract.CleanText(extractResult.Text)
				}
			}
		}

		text = extract.CleanText(text)
		if text == "" {
			continue
		}

		now := time.Now().UnixMilli()
		extractionID := p.newID()

		title := entry.Title
		url := entry.Link
		if url == "" {
			url = src.URL
		}

		// Store extraction.
		extraction := &store.Extraction{
			ID:            extractionID,
			SourceID:      src.ID,
			ContentHash:   contentHash,
			Title:         title,
			ExtractedText: text,
			URL:           url,
			ExtractedAt:   now,
		}
		if err := s.InsertExtraction(ctx, extraction); err != nil {
			log.Warn("rss: insert extraction failed", "error", err, "guid", entry.GUID)
			continue
		}

		// Write to buffer.
		if p.buffer != nil && p.currentJob != nil {
			meta := buffer.Metadata{
				ID:          extractionID,
				SourceID:    src.ID,
				DossierID:   p.currentJob.DossierID,
				SourceURL:   url,
				SourceType:  "rss",
				Title:       title,
				ContentHash: contentHash,
				ExtractedAt: time.Now().UTC(),
			}
			if _, err := p.buffer.Write(ctx, meta, text); err != nil {
				log.Warn("rss: buffer write failed", "error", err)
			}
		}

		newCount++
	}

	logEntry.Status = "ok"
	s.InsertFetchLog(ctx, logEntry)
	s.RecordFetchSuccess(ctx, src.ID, result.Hash)

	log.Info("rss: processed", "entries", len(f.Entries), "new", newCount, "duration_ms", duration)

	return nil
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
