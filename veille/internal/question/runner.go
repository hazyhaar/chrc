// CLAUDE:SUMMARY Executes tracked questions against search engines and stores deduplicated results.
// Package question implements the question runner for tracked questions.
//
// A tracked question is a search query replayed periodically on search engines,
// producing timestamped extractions for time-series analysis.
package question

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hazyhaar/chrc/extract"
	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/search"
	"github.com/hazyhaar/chrc/veille/internal/store"
)

// Runner executes tracked questions against search engines.
type Runner struct {
	engines  func(ctx context.Context, id string) (*search.Engine, error)
	searcher func(ctx context.Context, engine *search.Engine, query string) ([]search.Result, error)
	fetcher  *fetch.Fetcher
	buffer   *buffer.Writer
	logger   *slog.Logger
	newID    func() string
}

// Config holds dependencies for creating a Runner.
type Config struct {
	// Engines looks up a search engine by ID. Typically wraps store.GetSearchEngine.
	Engines func(ctx context.Context, id string) (*search.Engine, error)

	// Searcher executes a search query. If nil, defaults to search.Search with a 30s-timeout client.
	Searcher func(ctx context.Context, engine *search.Engine, query string) ([]search.Result, error)

	// Fetcher for following result links.
	Fetcher *fetch.Fetcher

	// Buffer for .md output (optional).
	Buffer *buffer.Writer

	Logger *slog.Logger
	NewID  func() string
}

// NewRunner creates a Runner with the given dependencies.
func NewRunner(cfg Config) *Runner {
	r := &Runner{
		engines:  cfg.Engines,
		searcher: cfg.Searcher,
		fetcher:  cfg.Fetcher,
		buffer:   cfg.Buffer,
		logger:   cfg.Logger,
		newID:    cfg.NewID,
	}
	if r.logger == nil {
		r.logger = slog.Default()
	}
	if r.searcher == nil {
		r.searcher = func(ctx context.Context, engine *search.Engine, query string) ([]search.Result, error) {
			return search.Search(ctx, engine, query, nil)
		}
	}
	return r
}

// Run executes a tracked question: searches each channel, deduplicates results,
// optionally follows links, stores extractions and chunks. Returns new result count.
func (r *Runner) Run(ctx context.Context, s *store.Store, q *store.TrackedQuestion, dossierID string) (int, error) {
	log := r.logger.With("question_id", q.ID, "text", q.Text)

	// Determine query.
	query := q.Keywords
	if query == "" {
		query = q.Text
	}

	// Parse channel IDs.
	var channelIDs []string
	if q.Channels != "" && q.Channels != "[]" {
		if err := json.Unmarshal([]byte(q.Channels), &channelIDs); err != nil {
			return 0, fmt.Errorf("parse channels: %w", err)
		}
	}
	if len(channelIDs) == 0 {
		log.Warn("question: no channels configured")
		return 0, nil
	}

	// Collect results from all engines.
	type taggedResult struct {
		result   search.Result
		engineID string
	}
	var allResults []taggedResult

	for _, engineID := range channelIDs {
		engine, err := r.engines(ctx, engineID)
		if err != nil {
			log.Warn("question: engine lookup failed", "engine_id", engineID, "error", err)
			continue
		}
		if engine == nil || !engine.Enabled {
			log.Debug("question: engine not found or disabled", "engine_id", engineID)
			continue
		}

		results, err := r.searcher(ctx, engine, query)
		if err != nil {
			log.Warn("question: search failed", "engine_id", engineID, "error", err)
			continue
		}

		for _, res := range results {
			allResults = append(allResults, taggedResult{result: res, engineID: engineID})
		}
	}

	// Limit to max_results.
	if q.MaxResults > 0 && len(allResults) > q.MaxResults {
		allResults = allResults[:q.MaxResults]
	}

	// Process each result.
	var newCount int
	for _, tr := range allResults {
		res := tr.result
		contentHash := hashString(res.URL)

		// Dedup: sourceID = q.ID.
		exists, err := s.ExtractionExists(ctx, q.ID, contentHash)
		if err != nil {
			log.Warn("question: dedup check failed", "error", err)
			continue
		}
		if exists {
			continue
		}

		// Get text content.
		var text string
		if q.FollowLinks && res.URL != "" && r.fetcher != nil {
			fetchResult, fetchErr := r.fetcher.Fetch(ctx, res.URL, "", "", "")
			if fetchErr == nil && fetchResult.Changed {
				extractResult, extractErr := extract.Extract(fetchResult.Body, extract.Options{Mode: "auto"})
				if extractErr == nil && extractResult.Text != "" {
					text = extract.CleanText(extractResult.Text)
				}
			}
		}
		if text == "" {
			text = extract.CleanText(res.Snippet)
		}
		if text == "" {
			continue
		}

		now := time.Now().UnixMilli()
		extractionID := r.newID()

		metaJSON, _ := json.Marshal(map[string]string{
			"question_id": q.ID,
			"channel":     tr.engineID,
			"query":       query,
		})

		extraction := &store.Extraction{
			ID:            extractionID,
			SourceID:      q.ID, // question IS the source
			ContentHash:   contentHash,
			Title:         res.Title,
			ExtractedText: text,
			URL:           res.URL,
			ExtractedAt:   now,
			MetadataJSON:  string(metaJSON),
		}
		if err := s.InsertExtraction(ctx, extraction); err != nil {
			log.Warn("question: insert extraction failed", "error", err, "url", res.URL)
			continue
		}

		// Buffer write.
		if r.buffer != nil {
			meta := buffer.Metadata{
				ID:          extractionID,
				SourceID:    q.ID,
				DossierID:   dossierID,
				SourceURL:   res.URL,
				SourceType:  "question",
				Title:       res.Title,
				ContentHash: contentHash,
				ExtractedAt: time.Now().UTC(),
			}
			if _, err := r.buffer.Write(ctx, meta, text); err != nil {
				log.Warn("question: buffer write failed", "error", err)
			}
		}

		newCount++
	}

	// Record run stats.
	if err := s.RecordQuestionRun(ctx, q.ID, newCount); err != nil {
		log.Warn("question: record run failed", "error", err)
	}

	log.Info("question: run complete", "new", newCount, "total_searched", len(allResults))
	return newCount, nil
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
