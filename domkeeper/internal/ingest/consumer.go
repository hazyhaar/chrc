// Package ingest implements the domwatch → domkeeper ingestion pipeline.
//
// Flow:
//  1. Receive Batch/Snapshot/Profile from domwatch (via callback sink)
//  2. Match extraction rules against the page URL
//  3. Extract content using the rule's selectors and mode
//  4. Deduplicate by content hash
//  5. Chunk extracted text for RAG/search
//  6. Store everything in SQLite
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hazyhaar/chrc/chunk"
	"github.com/hazyhaar/chrc/extract"
	"github.com/hazyhaar/chrc/domkeeper/internal/store"
	"github.com/hazyhaar/chrc/domwatch/mutation"
	"github.com/hazyhaar/pkg/idgen"
)

// Consumer processes domwatch events and feeds them into the store.
type Consumer struct {
	store   *store.Store
	logger  *slog.Logger
	newID   func() string
	chunkOpts chunk.Options
}

// Option configures a Consumer.
type Option func(*Consumer)

// WithLogger sets the consumer's logger.
func WithLogger(l *slog.Logger) Option {
	return func(c *Consumer) { c.logger = l }
}

// WithChunkOptions sets the chunk splitting parameters.
func WithChunkOptions(opts chunk.Options) Option {
	return func(c *Consumer) { c.chunkOpts = opts }
}

// New creates an ingestion consumer.
func New(s *store.Store, opts ...Option) *Consumer {
	c := &Consumer{
		store:  s,
		logger: slog.Default(),
		newID:  idgen.New,
		chunkOpts: chunk.Options{
			MaxTokens:      512,
			OverlapTokens:  64,
			MinChunkTokens: 32,
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// HandleBatch processes a mutation batch from domwatch.
// It re-extracts content from pages that have matching rules.
func (c *Consumer) HandleBatch(ctx context.Context, batch mutation.Batch) error {
	log := c.logger.With("batch_id", batch.ID, "page_url", batch.PageURL, "page_id", batch.PageID)
	log.Debug("ingest: batch received", "records", len(batch.Records), "seq", batch.Seq)

	entry := &store.IngestEntry{
		ID:           c.newID(),
		BatchID:      batch.ID,
		PageURL:      batch.PageURL,
		PageID:       batch.PageID,
		Status:       "processing",
		RecordsCount: len(batch.Records),
	}
	c.store.InsertIngestEntry(ctx, entry)

	// Update source page last_seen.
	if batch.PageID != "" {
		c.store.UpsertSourcePage(ctx, &store.SourcePage{
			PageID:  batch.PageID,
			PageURL: batch.PageURL,
		})
	}

	// For batches, we log the event but don't re-extract unless it's a significant change.
	// Significant: doc_reset, or many inserts/removes.
	significant := false
	for _, r := range batch.Records {
		if r.Op == mutation.OpDocReset || r.Op == mutation.OpInsert || r.Op == mutation.OpRemove {
			significant = true
			break
		}
	}

	if !significant {
		c.store.CompleteIngestEntry(ctx, entry.ID, "done", "", 0)
		log.Debug("ingest: batch skipped (no structural changes)")
		return nil
	}

	// Note: full re-extraction requires the current page HTML.
	// For batch-only events, we mark this for later snapshot-based extraction.
	c.store.CompleteIngestEntry(ctx, entry.ID, "done", "awaiting snapshot for extraction", 0)
	log.Debug("ingest: batch processed, awaiting snapshot")
	return nil
}

// HandleSnapshot processes a full DOM snapshot from domwatch.
// This is the primary extraction trigger.
func (c *Consumer) HandleSnapshot(ctx context.Context, snap mutation.Snapshot) error {
	log := c.logger.With("snapshot_id", snap.ID, "page_url", snap.PageURL, "page_id", snap.PageID)
	log.Info("ingest: snapshot received", "html_size", len(snap.HTML))

	entry := &store.IngestEntry{
		ID:         c.newID(),
		SnapshotID: snap.ID,
		PageURL:    snap.PageURL,
		PageID:     snap.PageID,
		Status:     "processing",
	}
	c.store.InsertIngestEntry(ctx, entry)

	// Update source page.
	if snap.PageID != "" {
		c.store.UpsertSourcePage(ctx, &store.SourcePage{
			PageID:  snap.PageID,
			PageURL: snap.PageURL,
		})
	}

	// Find matching rules.
	rules, err := c.store.MatchRules(ctx, snap.PageURL, snap.PageID)
	if err != nil {
		c.store.CompleteIngestEntry(ctx, entry.ID, "error", err.Error(), 0)
		return fmt.Errorf("match rules: %w", err)
	}

	if len(rules) == 0 {
		c.store.CompleteIngestEntry(ctx, entry.ID, "done", "no matching rules", 0)
		log.Debug("ingest: no matching rules for page")
		return nil
	}

	var extractedCount int
	for _, rule := range rules {
		n, err := c.extractAndStore(ctx, rule, snap)
		if err != nil {
			log.Warn("ingest: extraction failed", "rule_id", rule.ID, "rule_name", rule.Name, "error", err)
			c.store.RecordRuleFailure(ctx, rule.ID)
			continue
		}
		extractedCount += n
		c.store.RecordRuleSuccess(ctx, rule.ID)
	}

	c.store.CompleteIngestEntry(ctx, entry.ID, "done", "", extractedCount)
	log.Info("ingest: snapshot processed", "rules_matched", len(rules), "extracted", extractedCount)
	return nil
}

// HandleProfile processes a page profile from domwatch.
func (c *Consumer) HandleProfile(ctx context.Context, prof mutation.Profile) error {
	c.logger.Info("ingest: profile received", "page_url", prof.PageURL,
		"landmarks", len(prof.Landmarks), "dynamic_zones", len(prof.DynamicZones))

	profJSON, _ := json.Marshal(prof)

	// Auto-create extraction rules from profile if none exist.
	existing, err := c.store.MatchRules(ctx, prof.PageURL, "")
	if err != nil {
		return fmt.Errorf("match rules: %w", err)
	}

	if len(existing) == 0 && len(prof.ContentSelectors) > 0 {
		rule := &store.Rule{
			ID:          c.newID(),
			Name:        "auto:" + prof.PageURL,
			URLPattern:  prof.PageURL,
			Selectors:   prof.ContentSelectors,
			ExtractMode: "css",
			TrustLevel:  "unverified",
			Enabled:     true,
			Priority:    0,
		}
		if err := c.store.InsertRule(ctx, rule); err != nil {
			c.logger.Warn("ingest: auto-create rule failed", "error", err)
		} else {
			c.logger.Info("ingest: auto-created extraction rule from profile",
				"rule_id", rule.ID, "selectors", prof.ContentSelectors)
		}
	}

	// Store/update source page with profile data.
	c.store.UpsertSourcePage(ctx, &store.SourcePage{
		PageURL:     prof.PageURL,
		ProfileJSON: string(profJSON),
	})

	return nil
}

// extractAndStore runs the extraction pipeline for one rule against a snapshot.
func (c *Consumer) extractAndStore(ctx context.Context, rule *store.Rule, snap mutation.Snapshot) (int, error) {
	result, err := extract.Extract(snap.HTML, extract.Options{
		Selectors:  rule.Selectors,
		Mode:       rule.ExtractMode,
		MinTextLen: 50,
	})
	if err != nil {
		return 0, fmt.Errorf("extract: %w", err)
	}

	cleanText := extract.CleanText(result.Text)
	if cleanText == "" {
		return 0, nil
	}

	// Store content (dedup by hash).
	content := &store.Content{
		ID:            c.newID(),
		RuleID:        rule.ID,
		PageURL:       snap.PageURL,
		PageID:        snap.PageID,
		SnapshotRef:   snap.ID,
		ContentHash:   result.Hash,
		ExtractedText: cleanText,
		ExtractedHTML: result.HTML,
		Title:         result.Title,
		TrustLevel:    rule.TrustLevel,
	}

	isNew, err := c.store.InsertContent(ctx, content)
	if err != nil {
		return 0, fmt.Errorf("store content: %w", err)
	}
	if !isNew {
		return 0, nil // content unchanged, skip re-chunking
	}

	// Chunk the extracted text.
	chunks := chunk.Split(cleanText, c.chunkOpts)
	if len(chunks) == 0 {
		return 1, nil
	}

	storeChunks := make([]*store.Chunk, len(chunks))
	for i, ch := range chunks {
		storeChunks[i] = &store.Chunk{
			ID:          c.newID(),
			ContentID:   content.ID,
			ChunkIndex:  ch.Index,
			Text:        ch.Text,
			TokenCount:  ch.TokenCount,
			OverlapPrev: ch.OverlapPrev,
		}
	}

	if err := c.store.InsertChunks(ctx, storeChunks); err != nil {
		return 0, fmt.Errorf("store chunks: %w", err)
	}

	c.logger.Debug("ingest: content extracted and chunked",
		"rule_id", rule.ID, "content_id", content.ID, "chunks", len(chunks))

	return 1, nil
}

// ExtractFromHTML runs extraction directly on raw HTML for a specific rule.
// Used by the manual ingest command.
func (c *Consumer) ExtractFromHTML(ctx context.Context, ruleID string, pageURL string, rawHTML []byte) (int, error) {
	rule, err := c.store.GetRule(ctx, ruleID)
	if err != nil {
		return 0, fmt.Errorf("get rule: %w", err)
	}
	if rule == nil {
		return 0, fmt.Errorf("rule not found: %s", ruleID)
	}

	snap := mutation.Snapshot{
		ID:      c.newID(),
		PageURL: pageURL,
		HTML:    rawHTML,
	}

	return c.extractAndStore(ctx, rule, snap)
}
