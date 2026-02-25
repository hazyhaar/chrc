// CLAUDE:SUMMARY All store data types: Source, Extraction, FetchLogEntry, SearchEngine, TrackedQuestion, Stats.
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

// SearchResult is a FTS5 search hit on extractions.
type SearchResult struct {
	ExtractionID string  `json:"extraction_id"`
	SourceID     string  `json:"source_id"`
	Title        string  `json:"title"`
	Text         string  `json:"text"`
	Rank         float64 `json:"rank"`
}

// SpaceStats holds aggregate counters for a veille space.
type SpaceStats struct {
	Sources     int `json:"sources"`
	Extractions int `json:"extractions"`
	FetchLogs   int `json:"fetch_logs"`
}

// SearchEngine describes a search engine configuration.
type SearchEngine struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Strategy      string `json:"strategy"`       // "api" | "generic"
	URLTemplate   string `json:"url_template"`
	APIConfigJSON string `json:"api_config"`      // JSON string
	SelectorsJSON string `json:"selectors"`       // JSON string
	StealthLevel  int    `json:"stealth_level"`
	RateLimitMs   int64  `json:"rate_limit_ms"`
	MaxPages      int    `json:"max_pages"`
	Enabled       bool   `json:"enabled"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

// TrackedQuestion represents a question to be periodically searched.
type TrackedQuestion struct {
	ID              string `json:"id"`
	Text            string `json:"text"`
	Keywords        string `json:"keywords"`
	Channels        string `json:"channels"`          // JSON array of engine IDs
	ScheduleMs      int64  `json:"schedule_ms"`
	MaxResults      int    `json:"max_results"`
	FollowLinks     bool   `json:"follow_links"`
	Enabled         bool   `json:"enabled"`
	LastRunAt       *int64 `json:"last_run_at,omitempty"`
	LastResultCount int    `json:"last_result_count"`
	TotalResults    int    `json:"total_results"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

// SearchLogEntry records a user search query.
type SearchLogEntry struct {
	ID          string `json:"id"`
	Query       string `json:"query"`
	ResultCount int    `json:"result_count"`
	SearchedAt  int64  `json:"searched_at"`
}
