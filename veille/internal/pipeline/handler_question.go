// CLAUDE:SUMMARY Pipeline handler for question source type: runs tracked questions against search engines.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/question"
	"github.com/hazyhaar/chrc/veille/internal/store"
)

// QuestionHandler dispatches source_type="question" to the question.Runner.
type QuestionHandler struct {
	runner *question.Runner
}

// NewQuestionHandler creates a QuestionHandler wrapping the given runner.
func NewQuestionHandler(runner *question.Runner) *QuestionHandler {
	return &QuestionHandler{runner: runner}
}

// questionConfig is parsed from source.config_json for question sources.
type questionConfig struct {
	QuestionID string `json:"question_id"`
}

// Handle loads the tracked question from the store and runs it.
func (h *QuestionHandler) Handle(ctx context.Context, s *store.Store, src *store.Source, p *Pipeline) error {
	log := p.logger.With("source_id", src.ID, "handler", "question")
	start := time.Now()

	// Parse config to get question ID.
	var cfg questionConfig
	if src.ConfigJSON != "" && src.ConfigJSON != "{}" {
		if err := json.Unmarshal([]byte(src.ConfigJSON), &cfg); err != nil {
			return fmt.Errorf("question config: %w", err)
		}
	}
	if cfg.QuestionID == "" {
		cfg.QuestionID = src.ID // fallback: source ID = question ID
	}

	// Load the question.
	q, err := s.GetQuestion(ctx, cfg.QuestionID)
	if err != nil {
		return fmt.Errorf("get question: %w", err)
	}
	if q == nil {
		log.Warn("question: tracked question not found", "question_id", cfg.QuestionID)
		return nil
	}

	// Determine dossier from current job.
	var dossierID string
	if p.currentJob != nil {
		dossierID = p.currentJob.DossierID
	}

	// Run the question.
	newCount, err := h.runner.Run(ctx, s, q, dossierID)
	duration := time.Since(start).Milliseconds()

	// Record fetch log.
	logEntry := &store.FetchLogEntry{
		ID:         p.newID(),
		SourceID:   src.ID,
		DurationMs: duration,
		FetchedAt:  time.Now().UnixMilli(),
	}

	if err != nil {
		logEntry.Status = "error"
		logEntry.ErrorMessage = err.Error()
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchError(ctx, src.ID, err.Error())
		return fmt.Errorf("question run: %w", err)
	}

	logEntry.Status = "ok"
	s.InsertFetchLog(ctx, logEntry)
	s.RecordFetchSuccess(ctx, src.ID, "")

	log.Info("question: handler complete", "new", newCount, "duration_ms", duration)
	return nil
}
