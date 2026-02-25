// CLAUDE:SUMMARY Pipeline orchestrator dispatching fetch jobs to source-type-specific handlers.
// Package pipeline orchestrates the fetch → extract → store workflow.
//
// It dispatches to source-type-specific handlers (web, rss, api, document).
// The web handler is the default fallback for unknown source types.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"

	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/store"
	"github.com/hazyhaar/pkg/idgen"
)

// Job represents a fetch task for one source in one shard.
type Job struct {
	DossierID string
	SourceID  string
	URL       string
}

// Pipeline processes fetch jobs, dispatching to type-specific handlers.
type Pipeline struct {
	fetcher     *fetch.Fetcher
	logger      *slog.Logger
	newID       func() string
	buffer      *buffer.Writer
	handlers    map[string]SourceHandler
	currentJob  *Job // set during HandleJob for handlers to access
	mdConverter *converter.Converter
}

// New creates a Pipeline.
func New(fetcher *fetch.Fetcher, logger *slog.Logger) *Pipeline {
	if logger == nil {
		logger = slog.Default()
	}
	p := &Pipeline{
		fetcher: fetcher,
		logger:  logger,
		newID:   idgen.New,
		mdConverter: converter.NewConverter(
			converter.WithPlugins(
				base.NewBasePlugin(),
				commonmark.NewCommonmarkPlugin(),
				table.NewTablePlugin(),
			),
		),
		handlers: make(map[string]SourceHandler),
	}
	// Register built-in handlers.
	// "api" is now a connectivity service (api_fetch), auto-discovered by DiscoverHandlers.
	p.handlers["web"] = &WebHandler{}
	p.handlers["rss"] = &RSSHandler{}
	p.handlers["document"] = NewDocumentHandler()
	return p
}

// RegisteredTypes returns all registered source type names.
func (p *Pipeline) RegisteredTypes() []string {
	types := make([]string, 0, len(p.handlers))
	for k := range p.handlers {
		types = append(types, k)
	}
	return types
}

// SetBuffer configures the optional .md buffer writer.
// If set, each successful extraction also writes a .md file to the buffer.
func (p *Pipeline) SetBuffer(w *buffer.Writer) {
	p.buffer = w
}

// RegisterHandler registers a handler for a source type.
func (p *Pipeline) RegisterHandler(sourceType string, h SourceHandler) {
	p.handlers[sourceType] = h
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

	// Set current job for handlers to access dossier context.
	p.currentJob = job
	defer func() { p.currentJob = nil }()

	// Dispatch to source-type-specific handler.
	handler, ok := p.handlers[src.SourceType]
	if !ok {
		// Fallback to web handler for unknown types.
		handler = p.handlers["web"]
		log.Debug("pipeline: no handler for source_type, falling back to web",
			"source_type", src.SourceType)
	}

	return handler.Handle(ctx, s, src, p)
}

// htmlToMarkdown converts HTML to structured markdown.
// If conversion fails or produces empty output, returns the fallback plain text.
func (p *Pipeline) htmlToMarkdown(html string, sourceURL string, fallback string) string {
	if html == "" {
		return fallback
	}
	result, err := p.mdConverter.ConvertString(html, converter.WithDomain(sourceURL))
	if err != nil || strings.TrimSpace(result) == "" {
		return fallback
	}
	return strings.TrimSpace(result)
}
