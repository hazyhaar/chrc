// Package observer implements per-page DOM observation combining CDP events
// and an injected MutationObserver for complete mutation capture.
package observer

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/hazyhaar/chrc/domwatch/internal/browser"
	"github.com/hazyhaar/chrc/domwatch/internal/sink"
	"github.com/hazyhaar/chrc/domwatch/mutation"
	"github.com/hazyhaar/pkg/idgen"
)

//go:embed observer.js
var observerJS []byte

// Observer manages CDP + JS observation for a single page.
type Observer struct {
	tab    *browser.Tab
	sink   sink.Sink
	logger *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// Node tracking.
	nodes *nodeMap

	// Raw mutation channel (from both CDP and JS sources).
	rawCh      chan rawRecord
	docResetCh chan struct{}

	// Debouncing.
	debouncer *debouncer
	dedup     *deduper

	// Sequence counter (monotonically increasing per page).
	seq atomic.Uint64

	// Last snapshot ID for batch chaining.
	snapshotRef atomic.Value // stores string

	// Snapshot interval.
	snapshotInterval time.Duration

	// Filters.
	filters []string
}

// Config for creating an Observer.
type Config struct {
	Tab              *browser.Tab
	Sink             sink.Sink
	DebounceWindow   time.Duration
	DebounceMax      int
	SnapshotInterval time.Duration
	Filters          []string
	Logger           *slog.Logger
}

// New creates an Observer for the given tab.
func New(cfg Config) *Observer {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.SnapshotInterval <= 0 {
		cfg.SnapshotInterval = 4 * time.Hour
	}

	ctx, cancel := context.WithCancel(context.Background())

	o := &Observer{
		tab:              cfg.Tab,
		sink:             cfg.Sink,
		logger:           cfg.Logger,
		ctx:              ctx,
		cancel:           cancel,
		nodes:            newNodeMap(),
		rawCh:            make(chan rawRecord, 4096),
		docResetCh:       make(chan struct{}, 1),
		dedup:            newDeduper(),
		snapshotInterval: cfg.SnapshotInterval,
		filters:          cfg.Filters,
	}

	o.debouncer = newDebouncer(debounceConfig{
		Window:    cfg.DebounceWindow,
		MaxBuffer: cfg.DebounceMax,
	}, o.onFlush)

	return o
}

// Start begins observing the page. It:
// 1. Enables DOM tracking (getDocument depth=-1)
// 2. Subscribes to CDP DOM events
// 3. Injects the JS MutationObserver
// 4. Emits an initial snapshot
// 5. Runs the main loop (dedup → debounce → emit)
func (o *Observer) Start() error {
	// Enable DOM tracking.
	if err := o.initDOMTracking(); err != nil {
		return fmt.Errorf("observer: init DOM tracking: %w", err)
	}

	// Start CDP event listeners.
	cdp := newCDPListener(o)
	cdp.start()

	// Inject JS observer.
	if err := o.injectJS(); err != nil {
		return fmt.Errorf("observer: inject JS: %w", err)
	}

	// Initial snapshot.
	o.emitSnapshot()

	// Main processing loop.
	go o.loop()

	return nil
}

// Stop gracefully stops the observer, flushing remaining mutations.
func (o *Observer) Stop() {
	o.debouncer.flush()
	o.cancel()
}

func (o *Observer) initDOMTracking() error {
	page := o.tab.Page

	// DOM.getDocument with depth=-1 and pierce=true to track all nodes.
	// This is the critical CDP constraint: without it, mutations on deep
	// nodes are silently ignored.
	depth := -1
	doc, err := proto.DOMGetDocument{Depth: &depth, Pierce: true}.Call(page)
	if err != nil {
		return fmt.Errorf("DOM.getDocument: %w", err)
	}

	o.nodes.buildFromDocument(doc.Root)
	o.logger.Info("observer: DOM tracking initialised",
		"url", o.tab.PageURL,
		"nodes", len(o.nodes.paths))
	return nil
}

func (o *Observer) injectJS() error {
	// Set up the binding for JS → Go communication.
	err := proto.RuntimeAddBinding{Name: "__domwatcher_binding"}.Call(o.tab.Page)
	if err != nil {
		o.logger.Warn("observer: addBinding failed (may already exist)", "error", err)
	}

	// Listen for binding calls.
	go o.listenBinding()

	// Set up filters before injecting.
	if len(o.filters) > 0 {
		filtersJSON, _ := json.Marshal(o.filters)
		filterSetup := fmt.Sprintf("window.__domwatcher_filters = %s;", filtersJSON)
		_, err = o.tab.Page.Eval(filterSetup)
		if err != nil {
			o.logger.Warn("observer: set filters failed", "error", err)
		}
	}

	// Inject the observer JS.
	_, err = o.tab.Page.Eval(string(observerJS))
	if err != nil {
		return fmt.Errorf("inject observer.js: %w", err)
	}

	o.logger.Debug("observer: JS injected", "url", o.tab.PageURL)
	return nil
}

// listenBinding receives calls from the JS MutationObserver via Runtime.bindingCalled.
func (o *Observer) listenBinding() {
	page := o.tab.Page
	page.Context(o.ctx).EachEvent(func(e *proto.RuntimeBindingCalled) {
		if e.Name != "__domwatcher_binding" {
			return
		}

		var records []json.RawMessage
		if err := json.Unmarshal([]byte(e.Payload), &records); err != nil {
			o.logger.Warn("observer: parse JS binding payload", "error", err)
			return
		}

		now := time.Now()
		for _, raw := range records {
			var jsRec struct {
				Op       string `json:"op"`
				XPath    string `json:"xpath"`
				NodeType int    `json:"node_type"`
				Tag      string `json:"tag"`
				Name     string `json:"name"`
				Value    string `json:"value"`
				OldValue string `json:"old_value"`
				HTML     string `json:"html"`
			}
			if err := json.Unmarshal(raw, &jsRec); err != nil {
				continue
			}

			// Handle special signals from JS hooks.
			switch jsRec.Op {
			case "__navigate":
				go o.handleNavigate(jsRec.Value)
				continue
			case "__shadow":
				go o.handleShadowRoot(jsRec.XPath)
				continue
			}

			rec := mutation.Record{
				Op:       mutation.Op(jsRec.Op),
				XPath:    jsRec.XPath,
				NodeType: jsRec.NodeType,
				Tag:      jsRec.Tag,
				Name:     jsRec.Name,
				Value:    jsRec.Value,
				OldValue: jsRec.OldValue,
				HTML:     jsRec.HTML,
			}

			o.rawCh <- rawRecord{record: rec, source: sourceJS, at: now}
		}
	})()
}

// loop is the main event loop: reads raw records, deduplicates, debounces.
func (o *Observer) loop() {
	snapTicker := time.NewTicker(o.snapshotInterval)
	defer snapTicker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return

		case rr := <-o.rawCh:
			if o.dedup.isDuplicate(rr) {
				continue
			}
			o.debouncer.add(rr.record)

		case <-o.debouncer.timerC():
			o.debouncer.flush()

		case <-o.docResetCh:
			o.handleDocReset()

		case <-snapTicker.C:
			o.emitSnapshot()
		}
	}
}

// onFlush is called by the debouncer when a batch is ready.
func (o *Observer) onFlush(records []mutation.Record) {
	o.emitBatch(records)
}

func (o *Observer) emitBatch(records []mutation.Record) {
	if len(records) == 0 {
		return
	}

	ref := ""
	if v := o.snapshotRef.Load(); v != nil {
		ref = v.(string)
	}

	batch := mutation.Batch{
		ID:          idgen.New(),
		PageURL:     o.tab.PageURL,
		PageID:      o.tab.PageID,
		Seq:         o.seq.Add(1),
		Records:     records,
		Timestamp:   time.Now().UnixMilli(),
		SnapshotRef: ref,
	}

	if err := o.sink.Send(o.ctx, batch); err != nil {
		o.logger.Error("observer: send batch failed", "error", err)
	}
}

func (o *Observer) emitSnapshot() {
	html, err := o.tab.GetFullDOM(o.ctx)
	if err != nil {
		o.logger.Error("observer: get DOM for snapshot", "error", err)
		return
	}

	snap := mutation.Snapshot{
		ID:        idgen.New(),
		PageURL:   o.tab.PageURL,
		PageID:    o.tab.PageID,
		HTML:      html,
		HTMLHash:  mutation.HashHTML(html),
		Timestamp: time.Now().UnixMilli(),
	}

	o.snapshotRef.Store(snap.ID)

	if err := o.sink.SendSnapshot(o.ctx, snap); err != nil {
		o.logger.Error("observer: send snapshot failed", "error", err)
	}

	o.logger.Info("observer: snapshot emitted",
		"url", o.tab.PageURL, "id", snap.ID, "size", len(html))
}

// SetContext allows the parent watcher to pass its context.
func (o *Observer) SetContext(ctx context.Context) {
	o.ctx, o.cancel = context.WithCancel(ctx)
}
