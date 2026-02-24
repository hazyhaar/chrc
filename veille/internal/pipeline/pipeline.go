// Package pipeline orchestrates the fetch → extract → chunk → store workflow.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hazyhaar/chrc/chunk"
	"github.com/hazyhaar/chrc/extract"
	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/store"
	"github.com/hazyhaar/pkg/idgen"
)

// Job represents a fetch task for one source in one shard.
type Job struct {
	UserID   string
	SpaceID  string
	SourceID string
	URL      string
}

// Pipeline processes fetch jobs.
type Pipeline struct {
	fetcher   *fetch.Fetcher
	chunkOpts chunk.Options
	logger    *slog.Logger
	newID     func() string
}

// New creates a Pipeline.
func New(fetcher *fetch.Fetcher, chunkOpts chunk.Options, logger *slog.Logger) *Pipeline {
	if logger == nil {
		logger = slog.Default()
	}
	return &Pipeline{
		fetcher:   fetcher,
		chunkOpts: chunkOpts,
		logger:    logger,
		newID:     idgen.New,
	}
}

// HandleJob processes a single fetch job against a resolved shard store.
// Returns nil if the source is disabled or content is unchanged.
func (p *Pipeline) HandleJob(ctx context.Context, s *store.Store, job *Job) error {
	log := p.logger.With("source_id", job.SourceID, "url", job.URL)

	src, err := s.GetSource(ctx, job.SourceID)
	if err != nil {
		return fmt.Errorf("get source: %w", err)
	}
	if src == nil {
		log.Warn("pipeline: source not found, skipping")
		return nil
	}
	if !src.Enabled {
		log.Debug("pipeline: source disabled, skipping")
		return nil
	}

	start := time.Now()

	// Fetch with conditional GET.
	result, err := p.fetcher.Fetch(ctx, src.URL, "", "", src.LastHash)

	duration := time.Since(start).Milliseconds()

	// Log the fetch attempt.
	logEntry := &store.FetchLogEntry{
		ID:         p.newID(),
		SourceID:   job.SourceID,
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
		s.RecordFetchError(ctx, job.SourceID, err.Error())
		log.Warn("pipeline: fetch failed", "error", err, "duration_ms", duration)
		return fmt.Errorf("fetch: %w", err)
	}

	logEntry.StatusCode = result.StatusCode
	logEntry.ContentHash = result.Hash

	if !result.Changed {
		logEntry.Status = "unchanged"
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchUnchanged(ctx, job.SourceID)
		log.Debug("pipeline: content unchanged", "duration_ms", duration)
		return nil
	}

	// Extract content.
	extractResult, err := extract.Extract(result.Body, extract.Options{
		Mode: "auto",
	})
	if err != nil {
		logEntry.Status = "extract_error"
		logEntry.ErrorMessage = err.Error()
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchError(ctx, job.SourceID, "extract: "+err.Error())
		log.Warn("pipeline: extraction failed", "error", err)
		return fmt.Errorf("extract: %w", err)
	}

	cleanText := extract.CleanText(extractResult.Text)
	if cleanText == "" {
		logEntry.Status = "empty"
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchSuccess(ctx, job.SourceID, result.Hash)
		log.Debug("pipeline: extracted text is empty")
		return nil
	}

	now := time.Now().UnixMilli()

	// Store extraction.
	extraction := &store.Extraction{
		ID:            p.newID(),
		SourceID:      job.SourceID,
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

	// Chunk the text.
	chunks := chunk.Split(cleanText, p.chunkOpts)
	if len(chunks) > 0 {
		storeChunks := make([]*store.Chunk, len(chunks))
		for i, ch := range chunks {
			storeChunks[i] = &store.Chunk{
				ID:           p.newID(),
				ExtractionID: extraction.ID,
				SourceID:     job.SourceID,
				ChunkIndex:   ch.Index,
				Text:         ch.Text,
				TokenCount:   ch.TokenCount,
				OverlapPrev:  ch.OverlapPrev,
				CreatedAt:    now,
			}
		}
		if err := s.InsertChunks(ctx, storeChunks); err != nil {
			return fmt.Errorf("store chunks: %w", err)
		}
	}

	logEntry.Status = "ok"
	s.InsertFetchLog(ctx, logEntry)
	s.RecordFetchSuccess(ctx, job.SourceID, result.Hash)

	log.Info("pipeline: processed",
		"chunks", len(chunks), "text_len", len(cleanText), "duration_ms", duration)

	return nil
}
