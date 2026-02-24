// Package veille provides multi-tenant content monitoring and indexing.
//
// It watches web sources, detects changes, extracts content, splits into
// RAG-ready chunks, and stores everything in per-user×space SQLite shards
// managed by usertenant.
package veille

import "github.com/hazyhaar/chrc/veille/internal/store"

// Re-export store types for public API.
type (
	Source        = store.Source
	Extraction    = store.Extraction
	Chunk         = store.Chunk
	FetchLogEntry = store.FetchLogEntry
	SearchResult  = store.SearchResult
	SpaceStats    = store.SpaceStats
)
