// CLAUDE:SUMMARY Re-exports store types (Source, Extraction, Stats, etc.) as the veille public API.
// Package veille provides multi-tenant content monitoring and indexing.
//
// It watches web sources, detects changes, extracts content, and stores
// everything in per-user×space SQLite shards managed by usertenant.
// FTS5 search runs directly on extractions. Buffer .md files are produced
// for downstream RAG consumption.
package veille

import (
	"github.com/hazyhaar/chrc/veille/internal/repair"
	"github.com/hazyhaar/chrc/veille/internal/store"
)

// Re-export store types for public API.
type (
	Source          = store.Source
	Extraction      = store.Extraction
	FetchLogEntry   = store.FetchLogEntry
	SearchResult    = store.SearchResult
	SpaceStats      = store.SpaceStats
	TrackedQuestion = store.TrackedQuestion
	SearchEngine    = store.SearchEngine
	SearchLogEntry  = store.SearchLogEntry
	SweepResult     = repair.SweepResult
)
