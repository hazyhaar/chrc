// Package schedule manages freshness checks and re-extraction scheduling.
//
// It periodically checks extraction rules that haven't been refreshed within
// their configured interval and publishes re-extraction jobs to VTQ.
package schedule

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hazyhaar/chrc/domkeeper/internal/store"
	"github.com/hazyhaar/pkg/idgen"
	"github.com/hazyhaar/pkg/vtq"
)

// Config controls the scheduler behaviour.
type Config struct {
	// CheckInterval is how often the scheduler checks for stale rules.
	CheckInterval time.Duration
	// DefaultFreshness is the max age before content is considered stale.
	DefaultFreshness time.Duration
	// MaxFailCount disables rules that fail too many times in a row.
	MaxFailCount int
}

func (c *Config) defaults() {
	if c.CheckInterval <= 0 {
		c.CheckInterval = 5 * time.Minute
	}
	if c.DefaultFreshness <= 0 {
		c.DefaultFreshness = 1 * time.Hour
	}
	if c.MaxFailCount <= 0 {
		c.MaxFailCount = 10
	}
}

// RefreshJob is the VTQ payload for a re-extraction task.
type RefreshJob struct {
	RuleID  string `json:"rule_id"`
	PageURL string `json:"page_url"`
	PageID  string `json:"page_id"`
}

// Scheduler checks for stale extraction rules and queues refresh jobs.
type Scheduler struct {
	store  *store.Store
	queue  *vtq.Q
	config Config
	logger *slog.Logger
}

// New creates a freshness scheduler.
func New(s *store.Store, q *vtq.Q, cfg Config, logger *slog.Logger) *Scheduler {
	cfg.defaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{store: s, queue: q, config: cfg, logger: logger}
}

// Run starts the scheduler loop. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.logger.Info("scheduler: started",
		"check_interval", s.config.CheckInterval,
		"default_freshness", s.config.DefaultFreshness)

	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler: stopped")
			return
		case <-ticker.C:
			if err := s.check(ctx); err != nil {
				s.logger.Warn("scheduler: check failed", "error", err)
			}
		}
	}
}

func (s *Scheduler) check(ctx context.Context) error {
	rules, err := s.store.ListRules(ctx, true) // enabled only
	if err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	staleThreshold := now - s.config.DefaultFreshness.Milliseconds()
	var queued int

	for _, rule := range rules {
		// Skip rules that have failed too many times.
		if rule.FailCount >= s.config.MaxFailCount {
			s.logger.Warn("scheduler: rule disabled due to failures",
				"rule_id", rule.ID, "fail_count", rule.FailCount)
			continue
		}

		// Check if content is stale.
		if rule.LastSuccess != nil && *rule.LastSuccess > staleThreshold {
			continue // still fresh
		}

		// Queue a refresh job.
		job := RefreshJob{
			RuleID:  rule.ID,
			PageURL: rule.URLPattern,
			PageID:  rule.PageID,
		}
		payload, _ := json.Marshal(job)

		if err := s.queue.Publish(ctx, idgen.New(), payload); err != nil {
			s.logger.Warn("scheduler: publish failed", "rule_id", rule.ID, "error", err)
			continue
		}
		queued++
	}

	if queued > 0 {
		s.logger.Info("scheduler: queued refresh jobs", "count", queued)
	}
	return nil
}
