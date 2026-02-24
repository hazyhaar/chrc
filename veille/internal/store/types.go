package store

// Source represents a monitored URL.
type Source struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	SourceType    string `json:"source_type"`
	FetchInterval int64  `json:"fetch_interval"` // ms
	Enabled       bool   `json:"enabled"`
	ConfigJSON    string `json:"config_json"`
	LastFetchedAt *int64 `json:"last_fetched_at,omitempty"`
	LastHash      string `json:"last_hash"`
	LastStatus    string `json:"last_status"`
	LastError     string `json:"last_error"`
	FailCount     int    `json:"fail_count"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

// Extraction represents content extracted from a source at a point in time.
type Extraction struct {
	ID            string `json:"id"`
	SourceID      string `json:"source_id"`
	ContentHash   string `json:"content_hash"`
	Title         string `json:"title"`
	ExtractedText string `json:"extracted_text"`
	ExtractedHTML string `json:"extracted_html"`
	URL           string `json:"url"`
	ExtractedAt   int64  `json:"extracted_at"`
	MetadataJSON  string `json:"metadata_json"`
}

// Chunk represents a text fragment for RAG consumption.
type Chunk struct {
	ID           string `json:"id"`
	ExtractionID string `json:"extraction_id"`
	SourceID     string `json:"source_id"`
	ChunkIndex   int    `json:"chunk_index"`
	Text         string `json:"text"`
	TokenCount   int    `json:"token_count"`
	OverlapPrev  int    `json:"overlap_prev"`
	CreatedAt    int64  `json:"created_at"`
}

// FetchLogEntry is one fetch attempt record.
type FetchLogEntry struct {
	ID           string `json:"id"`
	SourceID     string `json:"source_id"`
	Status       string `json:"status"`
	StatusCode   int    `json:"status_code"`
	ContentHash  string `json:"content_hash"`
	ErrorMessage string `json:"error_message"`
	DurationMs   int64  `json:"duration_ms"`
	FetchedAt    int64  `json:"fetched_at"`
}

// SearchResult is a FTS5 search hit.
type SearchResult struct {
	ChunkID      string  `json:"chunk_id"`
	SourceID     string  `json:"source_id"`
	ExtractionID string  `json:"extraction_id"`
	Text         string  `json:"text"`
	Rank         float64 `json:"rank"`
}

// SpaceStats holds aggregate counters for a veille space.
type SpaceStats struct {
	Sources     int `json:"sources"`
	Extractions int `json:"extractions"`
	Chunks      int `json:"chunks"`
	FetchLogs   int `json:"fetch_logs"`
}
