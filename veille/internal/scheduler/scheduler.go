// Package scheduler polls for due sources and enqueues fetch jobs.
package scheduler

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/store"
)

// Job is a fetch job emitted by the scheduler.
type Job struct {
	UserID   string `json:"user_id"`
	SpaceID  string `json:"space_id"`
	SourceID string `json:"source_id"`
	URL      string `json:"url"`
}

// Config configures the scheduler.
type Config struct {
	// CheckInterval is how often to poll for due sources. Default: 1 minute.
	CheckInterval time.Duration
	// MaxFailCount is the maximum failure count before a source is skipped.
	MaxFailCount int
}

func (c *Config) defaults() {
	if c.CheckInterval <= 0 {
		c.CheckInterval = time.Minute
	}
	if c.MaxFailCount <= 0 {
		c.MaxFailCount = 10
	}
}

// ShardResolver returns a *sql.DB for a given user×space.
type ShardResolver func(ctx context.Context, userID, spaceID string) (*sql.DB, error)

// ShardLister returns all active (userID, spaceID) pairs.
type ShardLister func(ctx context.Context) ([][2]string, error)

// JobSink receives enqueued jobs.
type JobSink func(ctx context.Context, job *Job) error

// Scheduler periodically checks for due sources across all shards.
type Scheduler struct {
	resolve ShardResolver
	list    ShardLister
	sink    JobSink
	config  Config
	logger  *slog.Logger
}

// New creates a Scheduler.
func New(resolve ShardResolver, list ShardLister, sink JobSink, cfg Config, logger *slog.Logger) *Scheduler {
	cfg.defaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		resolve: resolve,
		list:    list,
		sink:    sink,
		config:  cfg,
		logger:  logger,
	}
}

// Run polls for due sources on a ticker. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	// Run once immediately on start.
	s.enqueueDueSources(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.enqueueDueSources(ctx)
		}
	}
}

// enqueueDueSources iterates all active shards and enqueues due sources.
func (s *Scheduler) enqueueDueSources(ctx context.Context) {
	shards, err := s.list(ctx)
	if err != nil {
		s.logger.Error("scheduler: list shards", "error", err)
		return
	}

	for _, shard := range shards {
		userID, spaceID := shard[0], shard[1]

		db, err := s.resolve(ctx, userID, spaceID)
		if err != nil {
			s.logger.Warn("scheduler: resolve shard", "user", userID, "space", spaceID, "error", err)
			continue
		}

		st := store.NewStore(db)
		due, err := st.DueSources(ctx, s.config.MaxFailCount)
		if err != nil {
			s.logger.Warn("scheduler: due sources", "user", userID, "space", spaceID, "error", err)
			continue
		}

		for _, src := range due {
			job := &Job{
				UserID:   userID,
				SpaceID:  spaceID,
				SourceID: src.ID,
				URL:      src.URL,
			}
			if err := s.sink(ctx, job); err != nil {
				s.logger.Warn("scheduler: enqueue job", "source_id", src.ID, "error", err)
			}
		}

		if len(due) > 0 {
			s.logger.Debug("scheduler: enqueued", "user", userID, "space", spaceID, "jobs", len(due))
		}
	}
}
