// CLAUDE:SUMMARY Handles shadow root attachment signals from injected JS (open roots observed via binding).
package observer

// handleShadowRoot processes a shadow root attachment signal from the
// injected JS. Open shadow roots are already observed by the JS observer
// (it attaches a MutationObserver to the shadow root). Closed shadow roots
// are silently ignored (Same-Origin Policy makes them inaccessible).
func (o *Observer) handleShadowRoot(xpath string) {
	o.logger.Debug("observer: shadow root discovered", "xpath", xpath, "mode", "open")
	// The JS observer already attached a MutationObserver to it.
	// Nothing more to do from the Go side — mutations will flow through
	// the existing binding channel.
}
