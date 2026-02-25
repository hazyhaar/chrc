// CLAUDE:SUMMARY Defines the Snapshot type representing a complete serialized DOM photo.
package mutation

// Snapshot is a complete DOM photo. Emitted at startup, periodically
// (default 4h), and after every doc_reset. The raw HTML is the immutable
// asset — the founding principle of the canvas.
type Snapshot struct {
	ID        string `json:"id"`        // UUIDv7
	PageURL   string `json:"page_url"`
	PageID    string `json:"page_id"`
	HTML      []byte `json:"html"`      // full serialised DOM
	HTMLHash  string `json:"html_hash"` // SHA-256 hex
	Timestamp int64  `json:"timestamp"` // epoch milliseconds
}
