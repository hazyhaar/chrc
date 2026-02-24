package veille

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/pipeline"
	"github.com/hazyhaar/chrc/veille/internal/scheduler"
	"github.com/hazyhaar/chrc/veille/internal/store"
	"github.com/hazyhaar/pkg/idgen"
)

// PoolResolver abstracts usertenant.Pool.Resolve for testability.
type PoolResolver interface {
	Resolve(ctx context.Context, userID, spaceID string) (*sql.DB, error)
}

// SpaceManager abstracts usertenant.Pool space lifecycle.
type SpaceManager interface {
	CreateSpace(ctx context.Context, userID, spaceID, name string) error
	DeleteSpace(ctx context.Context, userID, spaceID string) error
	ListSpaces(ctx context.Context, userID string) ([]SpaceInfo, error)
}

// SpaceInfo describes a veille space.
type SpaceInfo struct {
	UserID  string `json:"user_id"`
	SpaceID string `json:"space_id"`
	Name    string `json:"name"`
}

// Service is the main veille orchestrator.
type Service struct {
	pool      PoolResolver
	spaces    SpaceManager
	fetcher   *fetch.Fetcher
	pipeline  *pipeline.Pipeline
	scheduler *scheduler.Scheduler
	logger    *slog.Logger
	config    *Config
	newID     func() string
}

// New creates a veille Service.
func New(pool PoolResolver, spaces SpaceManager, cfg *Config, logger *slog.Logger) (*Service, error) {
	if cfg == nil {
		cfg = defaultConfig()
	}
	cfg.defaults()
	if logger == nil {
		logger = slog.Default()
	}

	f := fetch.New(cfg.Fetch)
	p := pipeline.New(f, cfg.Chunk, logger)

	svc := &Service{
		pool:     pool,
		spaces:   spaces,
		fetcher:  f,
		pipeline: p,
		logger:   logger,
		config:   cfg,
		newID:    idgen.New,
	}

	// Create scheduler with shard resolution wired to pool.
	resolve := func(ctx context.Context, userID, spaceID string) (*sql.DB, error) {
		return pool.Resolve(ctx, userID, spaceID)
	}
	list := func(ctx context.Context) ([][2]string, error) {
		return svc.listActiveShards(ctx)
	}
	sink := func(ctx context.Context, job *scheduler.Job) error {
		return svc.processJob(ctx, job)
	}
	svc.scheduler = scheduler.New(resolve, list, sink, cfg.Scheduler, logger)

	return svc, nil
}

// Start launches the background scheduler. Non-blocking.
func (svc *Service) Start(ctx context.Context) {
	go svc.scheduler.Run(ctx)
	svc.logger.Info("veille: started")
}

// Close shuts down the service.
func (svc *Service) Close() error {
	svc.logger.Info("veille: closed")
	return nil
}

// resolveStore resolves a shard and wraps it in a Store.
func (svc *Service) resolveStore(ctx context.Context, userID, spaceID string) (*store.Store, error) {
	db, err := svc.pool.Resolve(ctx, userID, spaceID)
	if err != nil {
		return nil, fmt.Errorf("resolve shard: %w", err)
	}
	return store.NewStore(db), nil
}

// --- Sources ---

// AddSource adds a new monitored source to a space.
func (svc *Service) AddSource(ctx context.Context, userID, spaceID string, s *Source) error {
	if s.ID == "" {
		s.ID = svc.newID()
	}
	st, err := svc.resolveStore(ctx, userID, spaceID)
	if err != nil {
		return err
	}
	return st.InsertSource(ctx, s)
}

// ListSources returns all sources in a space.
func (svc *Service) ListSources(ctx context.Context, userID, spaceID string) ([]*Source, error) {
	st, err := svc.resolveStore(ctx, userID, spaceID)
	if err != nil {
		return nil, err
	}
	return st.ListSources(ctx)
}

// UpdateSource updates a source's mutable fields.
func (svc *Service) UpdateSource(ctx context.Context, userID, spaceID string, s *Source) error {
	st, err := svc.resolveStore(ctx, userID, spaceID)
	if err != nil {
		return err
	}
	return st.UpdateSource(ctx, s)
}

// DeleteSource removes a source and all its content.
func (svc *Service) DeleteSource(ctx context.Context, userID, spaceID, sourceID string) error {
	st, err := svc.resolveStore(ctx, userID, spaceID)
	if err != nil {
		return err
	}
	return st.DeleteSource(ctx, sourceID)
}

// FetchNow triggers an immediate fetch for a source.
func (svc *Service) FetchNow(ctx context.Context, userID, spaceID, sourceID string) error {
	st, err := svc.resolveStore(ctx, userID, spaceID)
	if err != nil {
		return err
	}
	src, err := st.GetSource(ctx, sourceID)
	if err != nil {
		return err
	}
	if src == nil {
		return fmt.Errorf("source not found: %s", sourceID)
	}
	return svc.pipeline.HandleJob(ctx, st, &pipeline.Job{
		UserID:   userID,
		SpaceID:  spaceID,
		SourceID: sourceID,
		URL:      src.URL,
	})
}

// --- Read operations ---

// Search performs FTS5 search on chunks.
func (svc *Service) Search(ctx context.Context, userID, spaceID, query string, limit int) ([]*SearchResult, error) {
	st, err := svc.resolveStore(ctx, userID, spaceID)
	if err != nil {
		return nil, err
	}
	return st.Search(ctx, query, limit)
}

// ListChunks returns chunks with pagination.
func (svc *Service) ListChunks(ctx context.Context, userID, spaceID string, limit, offset int) ([]*Chunk, error) {
	st, err := svc.resolveStore(ctx, userID, spaceID)
	if err != nil {
		return nil, err
	}
	return st.ListChunks(ctx, limit, offset)
}

// ListExtractions returns extractions for a source.
func (svc *Service) ListExtractions(ctx context.Context, userID, spaceID, sourceID string, limit int) ([]*Extraction, error) {
	st, err := svc.resolveStore(ctx, userID, spaceID)
	if err != nil {
		return nil, err
	}
	return st.ListExtractions(ctx, sourceID, limit)
}

// Stats returns aggregate counters for a space.
func (svc *Service) Stats(ctx context.Context, userID, spaceID string) (*SpaceStats, error) {
	st, err := svc.resolveStore(ctx, userID, spaceID)
	if err != nil {
		return nil, err
	}
	return st.Stats(ctx)
}

// FetchHistory returns fetch log entries for a source.
func (svc *Service) FetchHistory(ctx context.Context, userID, spaceID, sourceID string, limit int) ([]*FetchLogEntry, error) {
	st, err := svc.resolveStore(ctx, userID, spaceID)
	if err != nil {
		return nil, err
	}
	return st.FetchHistory(ctx, sourceID, limit)
}

// --- Spaces ---

// CreateSpace creates a new veille space for a user.
func (svc *Service) CreateSpace(ctx context.Context, userID, name string) (*SpaceInfo, error) {
	spaceID := svc.newID()
	if err := svc.spaces.CreateSpace(ctx, userID, spaceID, name); err != nil {
		return nil, err
	}
	// Apply veille schema on first resolve.
	db, err := svc.pool.Resolve(ctx, userID, spaceID)
	if err != nil {
		return nil, fmt.Errorf("resolve new space: %w", err)
	}
	if err := store.ApplySchema(db); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &SpaceInfo{UserID: userID, SpaceID: spaceID, Name: name}, nil
}

// ListSpaces returns all veille spaces for a user.
func (svc *Service) ListSpaces(ctx context.Context, userID string) ([]SpaceInfo, error) {
	return svc.spaces.ListSpaces(ctx, userID)
}

// DeleteSpace removes a veille space and its database.
func (svc *Service) DeleteSpace(ctx context.Context, userID, spaceID string) error {
	return svc.spaces.DeleteSpace(ctx, userID, spaceID)
}

// ApplySchema applies the veille schema to a database.
// Exported for use by usertenant factories and tests.
func ApplySchema(db *sql.DB) error {
	return store.ApplySchema(db)
}

// --- Internal ---

func (svc *Service) processJob(ctx context.Context, job *scheduler.Job) error {
	st, err := svc.resolveStore(ctx, job.UserID, job.SpaceID)
	if err != nil {
		return err
	}
	return svc.pipeline.HandleJob(ctx, st, &pipeline.Job{
		UserID:   job.UserID,
		SpaceID:  job.SpaceID,
		SourceID: job.SourceID,
		URL:      job.URL,
	})
}

func (svc *Service) listActiveShards(ctx context.Context) ([][2]string, error) {
	// Get all spaces across all users that the SpaceManager knows about.
	// For now, we rely on SpaceManager.ListSpaces but need all users.
	// This is a simplification — in production, usertenant catalog is queried directly.
	// The scheduler will be wired to the catalog in the binary.
	return nil, nil
}
