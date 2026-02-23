package observer

import (
	"time"

	"github.com/hazyhaar/pkg/domwatch/mutation"
)

// debounceConfig controls the batching behaviour.
type debounceConfig struct {
	// Window is the debounce time. Default: 250ms.
	Window time.Duration
	// MaxBuffer flushes immediately when this many records accumulate. Default: 1000.
	MaxBuffer int
}

func (dc *debounceConfig) defaults() {
	if dc.Window <= 0 {
		dc.Window = 250 * time.Millisecond
	}
	if dc.MaxBuffer <= 0 {
		dc.MaxBuffer = 1000
	}
}

// debouncer collects raw records and emits compressed Batches when the
// window expires or the buffer fills.
type debouncer struct {
	cfg     debounceConfig
	records []mutation.Record
	timer   *time.Timer
	timerCh <-chan time.Time
	flushFn func([]mutation.Record)
}

func newDebouncer(cfg debounceConfig, flushFn func([]mutation.Record)) *debouncer {
	cfg.defaults()
	return &debouncer{
		cfg:     cfg,
		records: make([]mutation.Record, 0, cfg.MaxBuffer),
		flushFn: flushFn,
	}
}

// add pushes a record into the buffer. Returns true if an immediate flush
// was triggered (buffer full).
func (d *debouncer) add(rec mutation.Record) bool {
	d.records = append(d.records, rec)

	if len(d.records) >= d.cfg.MaxBuffer {
		d.flush()
		return true
	}

	// (Re)start the window timer.
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.NewTimer(d.cfg.Window)
	d.timerCh = d.timer.C
	return false
}

// timerC returns the channel that fires when the debounce window expires.
func (d *debouncer) timerC() <-chan time.Time {
	return d.timerCh
}

// flush compresses and emits the buffered records, then resets.
func (d *debouncer) flush() {
	if len(d.records) == 0 {
		return
	}

	compressed := compress(d.records)
	d.flushFn(compressed)

	d.records = d.records[:0]
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
		d.timerCh = nil
	}
}

// compress applies the compression rules from the canvas:
// - N consecutive attr on same (xpath, name) → keep last (with old_value from first)
// - N consecutive text on same xpath → keep last
// - insert/remove never compressed (structurally significant)
func compress(records []mutation.Record) []mutation.Record {
	if len(records) <= 1 {
		return records
	}

	result := make([]mutation.Record, 0, len(records))

	for i := 0; i < len(records); i++ {
		rec := records[i]

		switch rec.Op {
		case mutation.OpAttr:
			// Look ahead for consecutive attr on same (xpath, name).
			firstOld := rec.OldValue
			j := i + 1
			for j < len(records) &&
				records[j].Op == mutation.OpAttr &&
				records[j].XPath == rec.XPath &&
				records[j].Name == rec.Name {
				rec = records[j]
				j++
			}
			rec.OldValue = firstOld
			result = append(result, rec)
			i = j - 1

		case mutation.OpText:
			// Look ahead for consecutive text on same xpath.
			firstOld := rec.OldValue
			j := i + 1
			for j < len(records) &&
				records[j].Op == mutation.OpText &&
				records[j].XPath == rec.XPath {
				rec = records[j]
				j++
			}
			rec.OldValue = firstOld
			result = append(result, rec)
			i = j - 1

		default:
			// insert, remove, attr_del, doc_reset: never compress.
			result = append(result, rec)
		}
	}

	return result
}
