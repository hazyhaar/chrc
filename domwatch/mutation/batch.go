// Package mutation defines the structured types emitted by domwatch.
// These are the public API contract: any consumer (domkeeper, custom pipelines)
// imports this package to receive and process DOM observations.
package mutation

// Op is the type of DOM mutation observed.
type Op string

const (
	OpInsert   Op = "insert"    // childNodeInserted (includes serialised subtree HTML)
	OpRemove   Op = "remove"    // childNodeRemoved
	OpText     Op = "text"      // characterDataModified
	OpAttr     Op = "attr"      // attributeModified
	OpAttrDel  Op = "attr_del"  // attributeRemoved
	OpDocReset Op = "doc_reset" // documentUpdated — entire DOM replaced
)

// Record is a single DOM mutation.
type Record struct {
	Op       Op     `json:"op"`
	XPath    string `json:"xpath"`
	NodeType int    `json:"node_type,omitempty"` // 1=element, 3=text, 8=comment
	Tag      string `json:"tag,omitempty"`
	Name     string `json:"name,omitempty"`      // attribute name for attr/attr_del
	Value    string `json:"value,omitempty"`      // new value
	OldValue string `json:"old_value,omitempty"` // previous value
	HTML     string `json:"html,omitempty"`      // serialised subtree for insert
}

// Batch is the atomic unit emitted by the watcher. One batch = all mutations
// collected during a single debounce window.
type Batch struct {
	ID          string   `json:"id"`           // UUIDv7
	PageURL     string   `json:"page_url"`
	PageID      string   `json:"page_id"`      // stable identifier provided by caller
	Seq         uint64   `json:"seq"`          // monotonically increasing per page (gap detection)
	Records     []Record `json:"records"`
	Timestamp   int64    `json:"timestamp"`    // epoch milliseconds at flush
	SnapshotRef string   `json:"snapshot_ref"` // ID of the last snapshot (chain incremental → snapshot)
}
