// Package domwatch provides a DOM observation daemon that orchestrates
// Chrome headless as a disposable component. It captures DOM mutations,
// produces snapshots, and profiles page structure.
//
// domwatch observes, it does not interpret. Raw HTML and structured deltas
// are emitted to sinks (stdout, webhook, callback) for consumers like
// domkeeper to process.
package domwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/go-rod/rod"
	"github.com/hazyhaar/pkg/connectivity"
	"github.com/hazyhaar/pkg/domwatch/internal/browser"
	"github.com/hazyhaar/pkg/domwatch/internal/config"
	"github.com/hazyhaar/pkg/domwatch/internal/fetcher"
	"github.com/hazyhaar/pkg/domwatch/internal/observer"
	"github.com/hazyhaar/pkg/domwatch/internal/profiler"
	"github.com/hazyhaar/pkg/domwatch/internal/sink"
	"github.com/hazyhaar/pkg/domwatch/mutation"
)

// Watcher is the top-level orchestrator. It manages the browser, observers,
// and sinks. Create one per domwatch instance.
type Watcher struct {
	cfg       *config.Config
	mgr       *browser.Manager
	fetch     *fetcher.Fetcher
	sinkR     *sink.Router
	observers map[string]*observer.Observer // keyed by page ID
	mu        sync.Mutex
	logger    *slog.Logger
}

// New creates a Watcher from configuration.
func New(cfg *config.Config, logger *slog.Logger, sinks ...sink.Sink) *Watcher {
	if logger == nil {
		logger = slog.Default()
	}

	stealthLevel := browser.LevelHeadless
	switch cfg.Browser.Stealth {
	case "headful":
		stealthLevel = browser.LevelHeadful
	case "headless":
		stealthLevel = browser.LevelHeadless
	}

	mgr := browser.NewManager(browser.Config{
		RemoteURL:        cfg.Browser.Remote,
		MemoryLimit:      cfg.Browser.MemoryLimit,
		RecycleInterval:  cfg.Browser.RecycleInterval,
		ResourceBlocking: cfg.Browser.ResourceBlocking,
		Stealth:          stealthLevel,
		XvfbDisplay:      cfg.Browser.XvfbDisplay,
		Logger:           logger,
	})

	return &Watcher{
		cfg:       cfg,
		mgr:       mgr,
		fetch:     fetcher.New(fetcher.WithLogger(logger)),
		sinkR:     sink.NewRouter(logger, sinks...),
		observers: make(map[string]*observer.Observer),
		logger:    logger,
	}
}

// Start launches the browser and begins observing all configured pages.
func (w *Watcher) Start(ctx context.Context) error {
	// Start browser.
	_, err := w.mgr.Start(ctx)
	if err != nil {
		return fmt.Errorf("domwatch: start browser: %w", err)
	}

	// Set up recycle callback to reconnect observers.
	w.mgr.SetRecycleCallback(&browser.RecycleCallback{
		BeforeRecycle: w.flushAllObservers,
		AfterRecycle:  func(b *rod.Browser) { w.reconnectObservers(ctx) },
	})

	// Start observing each configured page.
	for _, page := range w.cfg.Pages {
		if err := w.ObservePage(ctx, page); err != nil {
			w.logger.Error("domwatch: failed to observe page",
				"url", page.URL, "error", err)
		}
	}

	return nil
}

// ObservePage starts observing a single page.
func (w *Watcher) ObservePage(ctx context.Context, pageCfg config.PageConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Determine stealth level.
	level := w.resolveStealthLevel(ctx, pageCfg)

	if level == browser.LevelHTTP {
		// HTTP-only path: fetch once and produce a snapshot.
		return w.fetchHTTP(ctx, pageCfg)
	}

	// Browser path: open tab and start observer.
	tab, err := browser.OpenTab(ctx, w.mgr, pageCfg.URL, pageCfg.ID, level)
	if err != nil {
		return fmt.Errorf("domwatch: open tab: %w", err)
	}

	obs := observer.New(observer.Config{
		Tab:              tab,
		Sink:             w.sinkR,
		DebounceWindow:   w.cfg.Debounce.Window,
		DebounceMax:      w.cfg.Debounce.MaxBuffer,
		SnapshotInterval: pageCfg.SnapshotInterval,
		Filters:          pageCfg.Filters,
		Logger:           w.logger,
	})
	obs.SetContext(ctx)

	if err := obs.Start(); err != nil {
		tab.Close()
		return fmt.Errorf("domwatch: start observer: %w", err)
	}

	w.observers[pageCfg.ID] = obs

	// Profile if requested.
	if pageCfg.Profile {
		go w.profilePage(ctx, tab)
	}

	w.logger.Info("domwatch: observing page",
		"url", pageCfg.URL, "id", pageCfg.ID, "stealth", level)
	return nil
}

// ProfilePage runs the profiler on a URL and emits the result to sinks.
func (w *Watcher) ProfilePage(ctx context.Context, pageURL, pageID string) (*mutation.Profile, error) {
	// Open a fresh tab for profiling.
	tab, err := browser.OpenTab(ctx, w.mgr, pageURL, pageID, browser.LevelHeadless)
	if err != nil {
		return nil, fmt.Errorf("domwatch: profile open tab: %w", err)
	}
	defer tab.Close()

	prof, err := profiler.Profile(ctx, tab, profiler.Config{Logger: w.logger})
	if err != nil {
		return nil, err
	}

	if err := w.sinkR.SendProfile(ctx, *prof); err != nil {
		w.logger.Error("domwatch: send profile failed", "error", err)
	}

	return prof, nil
}

// Stop gracefully shuts down all observers and the browser.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for id, obs := range w.observers {
		obs.Stop()
		w.logger.Info("domwatch: stopped observer", "id", id)
	}
	w.observers = make(map[string]*observer.Observer)

	w.sinkR.Close()
	w.mgr.Close()
}

// resolveStealthLevel determines the appropriate stealth level for a page.
func (w *Watcher) resolveStealthLevel(ctx context.Context, pageCfg config.PageConfig) browser.StealthLevel {
	switch pageCfg.StealthLevel {
	case "0":
		return browser.LevelHTTP
	case "1":
		return browser.LevelHeadless
	case "2":
		return browser.LevelHeadful
	case "auto", "":
		// Try HTTP first. If content is insufficient, escalate.
		result, err := w.fetch.Fetch(ctx, pageCfg.URL, pageCfg.ID)
		if err != nil {
			w.logger.Warn("domwatch: auto-detect fetch failed, escalating to headless",
				"url", pageCfg.URL, "error", err)
			return browser.LevelHeadless
		}
		if result.Sufficient {
			return browser.LevelHTTP
		}
		w.logger.Info("domwatch: content insufficient via HTTP, escalating to headless",
			"url", pageCfg.URL)
		return browser.LevelHeadless
	default:
		return browser.LevelHeadless
	}
}

func (w *Watcher) fetchHTTP(ctx context.Context, pageCfg config.PageConfig) error {
	result, err := w.fetch.Fetch(ctx, pageCfg.URL, pageCfg.ID)
	if err != nil {
		return err
	}

	if err := w.sinkR.SendSnapshot(ctx, result.Snapshot); err != nil {
		return err
	}

	w.logger.Info("domwatch: HTTP snapshot emitted",
		"url", pageCfg.URL, "size", len(result.Snapshot.HTML))
	return nil
}

func (w *Watcher) profilePage(ctx context.Context, tab *browser.Tab) {
	prof, err := profiler.Profile(ctx, tab, profiler.Config{Logger: w.logger})
	if err != nil {
		w.logger.Error("domwatch: profile failed", "url", tab.PageURL, "error", err)
		return
	}
	if err := w.sinkR.SendProfile(ctx, *prof); err != nil {
		w.logger.Error("domwatch: send profile failed", "error", err)
	}
}

func (w *Watcher) flushAllObservers() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, obs := range w.observers {
		obs.Stop()
	}
}

func (w *Watcher) reconnectObservers(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Re-create observers for all pages.
	w.observers = make(map[string]*observer.Observer)
	for _, page := range w.cfg.Pages {
		if err := w.observePageLocked(ctx, page); err != nil {
			w.logger.Error("domwatch: reconnect observer failed",
				"url", page.URL, "error", err)
		}
	}
}

func (w *Watcher) observePageLocked(ctx context.Context, pageCfg config.PageConfig) error {
	level := browser.LevelHeadless // After recycle, use headless (not auto).
	tab, err := browser.OpenTab(ctx, w.mgr, pageCfg.URL, pageCfg.ID, level)
	if err != nil {
		return err
	}

	obs := observer.New(observer.Config{
		Tab:              tab,
		Sink:             w.sinkR,
		DebounceWindow:   w.cfg.Debounce.Window,
		DebounceMax:      w.cfg.Debounce.MaxBuffer,
		SnapshotInterval: pageCfg.SnapshotInterval,
		Filters:          pageCfg.Filters,
		Logger:           w.logger,
	})
	obs.SetContext(ctx)

	if err := obs.Start(); err != nil {
		tab.Close()
		return err
	}

	w.observers[pageCfg.ID] = obs
	return nil
}

// RegisterConnectivity registers domwatch services in the connectivity router.
// Services: domwatch_observe, domwatch_profile.
func (w *Watcher) RegisterConnectivity(router *connectivity.Router) {
	router.RegisterLocal("domwatch_observe", w.handleObserve)
	router.RegisterLocal("domwatch_profile", w.handleProfile)
}

// handleObserve is the connectivity handler for starting observation.
// Payload: {"page_id": "...", "url": "...", "stealth_level": "auto"}
func (w *Watcher) handleObserve(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		PageID       string `json:"page_id"`
		URL          string `json:"url"`
		StealthLevel string `json:"stealth_level"`
		Profile      bool   `json:"profile"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("domwatch_observe: unmarshal: %w", err)
	}

	pageCfg := config.PageConfig{
		ID:           req.PageID,
		URL:          req.URL,
		StealthLevel: req.StealthLevel,
		Profile:      req.Profile,
	}

	if err := w.ObservePage(ctx, pageCfg); err != nil {
		return nil, err
	}

	return json.Marshal(map[string]string{"status": "observing", "page_id": req.PageID})
}

// handleProfile is the connectivity handler for DOM profiling.
// Payload: {"url": "...", "page_id": "..."}
func (w *Watcher) handleProfile(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		URL    string `json:"url"`
		PageID string `json:"page_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("domwatch_profile: unmarshal: %w", err)
	}

	prof, err := w.ProfilePage(ctx, req.URL, req.PageID)
	if err != nil {
		return nil, err
	}

	return json.Marshal(prof)
}
