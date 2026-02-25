// CLAUDE:SUMMARY Main domkeeper orchestrator — wires store, ingestion consumer, scheduler, VTQ, and exposes search/rules API.
// Package domkeeper is the auto-repairing content extraction engine.
//
// It sits between domwatch (DOM observation) and downstream consumers (MCP tools,
// RAG pipelines, search). The pipeline:
//
//	domwatch → domkeeper.ingest → extract → chunk → store → search/MCP
//
// Key features:
//   - Extraction rules: CSS/XPath selectors or auto-density extraction
//   - Content deduplication: SHA-256 hash prevents redundant storage
//   - Overlapping chunks: RAG-ready text fragments with FTS5 search
//   - Auto-repair: profile-driven rule creation, failure tracking
//   - MCP tools: search, add/list/delete rules, folders, ingest status
//   - Connectivity: registers local handlers for inter-service routing
//
// Usage:
//
//	k, err := domkeeper.New(cfg, logger)
//	defer k.Close()
//	sink := k.Sink()  // plug into domwatch
//	k.RegisterMCP(mcpServer)
//	k.RegisterConnectivity(router)
//	k.Start(ctx)
package domkeeper

import (
	"context"
	"log/slog"

	"github.com/hazyhaar/chrc/chunk"
	"github.com/hazyhaar/chrc/domkeeper/internal/ingest"
	"github.com/hazyhaar/chrc/domkeeper/internal/schedule"
	"github.com/hazyhaar/chrc/domkeeper/internal/store"
	"github.com/hazyhaar/chrc/domwatch/mutation"
	"github.com/hazyhaar/pkg/vtq"
)

// Keeper is the main domkeeper orchestrator.
type Keeper struct {
	store     *store.Store
	consumer  *ingest.Consumer
	scheduler *schedule.Scheduler
	queue     *vtq.Q
	logger    *slog.Logger
	config    *Config
}

// New creates a Keeper instance. Opens the SQLite database and initialises
// the extraction pipeline, chunker, and VTQ queue.
func New(cfg *Config, logger *slog.Logger) (*Keeper, error) {
	cfg.defaults()
	if logger == nil {
		logger = slog.Default()
	}

	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	q := vtq.New(s.DB, vtq.Options{
		Queue:        "domkeeper_refresh",
		Visibility:   cfg.Scheduler.Visibility,
		PollInterval: cfg.Scheduler.PollInterval,
		Logger:       logger,
	})
	if err := q.EnsureTable(context.Background()); err != nil {
		s.Close()
		return nil, err
	}

	consumer := ingest.New(s,
		ingest.WithLogger(logger),
		ingest.WithChunkOptions(chunk.Options{
			MaxTokens:      cfg.Chunk.MaxTokens,
			OverlapTokens:  cfg.Chunk.OverlapTokens,
			MinChunkTokens: cfg.Chunk.MinChunkTokens,
		}),
	)

	sched := schedule.New(s, q, schedule.Config{
		CheckInterval:    cfg.Scheduler.CheckInterval,
		DefaultFreshness: cfg.Scheduler.DefaultFreshness,
		MaxFailCount:     cfg.Scheduler.MaxFailCount,
	}, logger)

	return &Keeper{
		store:     s,
		consumer:  consumer,
		scheduler: sched,
		queue:     q,
		logger:    logger,
		config:    cfg,
	}, nil
}

// Start launches background goroutines (scheduler, VTQ consumer).
func (k *Keeper) Start(ctx context.Context) {
	go k.scheduler.Run(ctx)
	k.logger.Info("domkeeper: started", "db", k.config.DBPath)
}

// Close shuts down the keeper and closes the database.
func (k *Keeper) Close() error {
	return k.store.Close()
}

// Store returns the underlying store for direct access (testing, admin).
func (k *Keeper) Store() *store.Store {
	return k.store
}

// HandleBatch processes a domwatch mutation batch.
func (k *Keeper) HandleBatch(ctx context.Context, batch mutation.Batch) error {
	return k.consumer.HandleBatch(ctx, batch)
}

// HandleSnapshot processes a domwatch DOM snapshot.
func (k *Keeper) HandleSnapshot(ctx context.Context, snap mutation.Snapshot) error {
	return k.consumer.HandleSnapshot(ctx, snap)
}

// HandleProfile processes a domwatch page profile.
func (k *Keeper) HandleProfile(ctx context.Context, prof mutation.Profile) error {
	return k.consumer.HandleProfile(ctx, prof)
}

// Search performs a full-text search on extracted content.
func (k *Keeper) Search(ctx context.Context, opts store.SearchOptions) ([]*store.SearchResult, error) {
	return k.store.Search(ctx, opts)
}

// AddRule creates a new extraction rule.
func (k *Keeper) AddRule(ctx context.Context, rule *store.Rule) error {
	return k.store.InsertRule(ctx, rule)
}

// GetRule retrieves an extraction rule by ID.
func (k *Keeper) GetRule(ctx context.Context, id string) (*store.Rule, error) {
	return k.store.GetRule(ctx, id)
}

// ListRules lists extraction rules.
func (k *Keeper) ListRules(ctx context.Context, enabledOnly bool) ([]*store.Rule, error) {
	return k.store.ListRules(ctx, enabledOnly)
}

// DeleteRule removes an extraction rule and its content.
func (k *Keeper) DeleteRule(ctx context.Context, id string) error {
	return k.store.DeleteRule(ctx, id)
}

// AddFolder creates a new content folder.
func (k *Keeper) AddFolder(ctx context.Context, f *store.Folder) error {
	return k.store.InsertFolder(ctx, f)
}

// ListFolders lists all content folders.
func (k *Keeper) ListFolders(ctx context.Context) ([]*store.Folder, error) {
	return k.store.ListFolders(ctx)
}

// Stats returns current store statistics.
func (k *Keeper) Stats(ctx context.Context) (*Stats, error) {
	chunks, err := k.store.CountChunks(ctx)
	if err != nil {
		return nil, err
	}
	content, err := k.store.CountContent(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := k.store.ListRules(ctx, false)
	if err != nil {
		return nil, err
	}
	folders, err := k.store.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	pages, err := k.store.ListSourcePages(ctx)
	if err != nil {
		return nil, err
	}

	return &Stats{
		Rules:       len(rules),
		Folders:     len(folders),
		Content:     content,
		Chunks:      chunks,
		SourcePages: len(pages),
	}, nil
}

// Stats holds domkeeper counts.
type Stats struct {
	Rules       int `json:"rules"`
	Folders     int `json:"folders"`
	Content     int `json:"content"`
	Chunks      int `json:"chunks"`
	SourcePages int `json:"source_pages"`
}
