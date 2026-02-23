package observer

import (
	"time"

	"github.com/hazyhaar/pkg/domwatch/mutation"
)

// recordSource distinguishes where a mutation came from.
type recordSource int

const (
	sourceCDP recordSource = iota
	sourceJS
)

// rawRecord is an unprocessed mutation with its source and arrival time.
type rawRecord struct {
	record mutation.Record
	source recordSource
	at     time.Time
}

// deduper removes duplicate mutations from CDP and MutationObserver.
// Key: (xpath, op, timestamp ± tolerance). Priority: JS (richer context).
type deduper struct {
	// tolerance is the time window within which two records with the same
	// (xpath, op) are considered duplicates. Default: 50ms.
	tolerance time.Duration
	// recent tracks recently seen records for dedup.
	recent []recentEntry
	// maxRecent caps the dedup window size.
	maxRecent int
}

type recentEntry struct {
	xpath  string
	op     mutation.Op
	name   string // attribute name for attr ops
	at     time.Time
	source recordSource
}

func newDeduper() *deduper {
	return &deduper{
		tolerance: 50 * time.Millisecond,
		maxRecent: 500,
	}
}

// isDuplicate returns true if this record is a duplicate of a recently
// seen record from the other source. When duplicate, JS source wins
// (richer context with old_value, attributeName).
func (d *deduper) isDuplicate(rr rawRecord) bool {
	now := rr.at
	if now.IsZero() {
		now = time.Now()
	}

	key := recentEntry{
		xpath:  rr.record.XPath,
		op:     rr.record.Op,
		name:   rr.record.Name,
		at:     now,
		source: rr.source,
	}

	// Prune old entries.
	cutoff := now.Add(-2 * d.tolerance)
	fresh := d.recent[:0]
	for _, e := range d.recent {
		if e.at.After(cutoff) {
			fresh = append(fresh, e)
		}
	}
	d.recent = fresh

	// Check for duplicate from the other source.
	for _, e := range d.recent {
		if e.xpath == key.xpath && e.op == key.op && e.name == key.name &&
			e.source != key.source &&
			absDuration(e.at.Sub(key.at)) <= d.tolerance {
			// Duplicate found. Keep the JS version (richer context).
			if rr.source == sourceCDP {
				return true // CDP duplicate — discard it
			}
			return false // JS version replaces — keep it
		}
	}

	// No duplicate — register and keep.
	d.recent = append(d.recent, key)
	if len(d.recent) > d.maxRecent {
		d.recent = d.recent[len(d.recent)-d.maxRecent:]
	}
	return false
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
