package domkeeper

import "github.com/hazyhaar/chrc/domkeeper/internal/store"

// Re-exported types from internal/store for use by cmd/ and external callers.
type (
	SearchOptions = store.SearchOptions
	SearchResult  = store.SearchResult
	Rule          = store.Rule
	Folder        = store.Folder
	Content       = store.Content
	Chunk         = store.Chunk
	IngestEntry   = store.IngestEntry
	SourcePage    = store.SourcePage
	GPUPricing    = store.GPUPricing
	GPUThreshold  = store.GPUThreshold
	SearchTierLog = store.SearchTierLog
)
