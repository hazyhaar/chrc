package observer

import (
	"time"

	"github.com/hazyhaar/chrc/domwatch/mutation"
)

// handleNavigate processes a SPA navigation signal from the injected JS.
// It waits for DOM stabilisation (no mutations for the settle duration),
// then signals a new snapshot.
func (o *Observer) handleNavigate(newURL string) {
	o.logger.Info("observer: SPA navigation detected", "url", newURL)
	o.tab.PageURL = newURL

	// Wait for DOM to settle — no mutations for 500ms.
	settle := 500 * time.Millisecond
	timer := time.NewTimer(settle)
	defer timer.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case rr := <-o.rawCh:
			// Mutations still arriving — keep processing them normally and reset timer.
			if !o.dedup.isDuplicate(rr) {
				o.debouncer.add(rr.record)
			}
			timer.Reset(settle)
		case <-timer.C:
			// DOM has settled — flush pending mutations and take a new snapshot.
			o.debouncer.flush()
			o.emitSnapshot()
			return
		}
	}
}

// handleDocReset processes a DOM.documentUpdated event. The entire DOM
// has been replaced (e.g. document.open/write). We need to:
// 1. Emit a doc_reset record
// 2. Rebuild the node map
// 3. Re-inject the MutationObserver
// 4. Take a new snapshot
func (o *Observer) handleDocReset() {
	o.logger.Info("observer: document updated (doc_reset)")

	// Flush any pending mutations.
	o.debouncer.flush()

	// Emit the doc_reset record as its own batch.
	o.emitBatch([]mutation.Record{{Op: mutation.OpDocReset}})

	// Re-initialise DOM tracking. Need to get the new document.
	if err := o.initDOMTracking(); err != nil {
		o.logger.Error("observer: re-init DOM tracking failed", "error", err)
		return
	}

	// Re-inject the JS observer.
	if err := o.injectJS(); err != nil {
		o.logger.Error("observer: re-inject JS failed", "error", err)
	}

	// New snapshot after reset.
	o.emitSnapshot()
}
