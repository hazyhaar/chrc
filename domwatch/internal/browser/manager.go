// CLAUDE:SUMMARY Manages Chrome headless lifecycle: launch, memory monitoring, time-based recycling, and crash recovery.
// Package browser manages Chrome headless-shell lifecycle: start, connect
// via Rod, monitor memory, recycle on threshold or interval, reconnect
// transparently after crash.
package browser

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// StealthLevel controls the browser automation mode.
type StealthLevel int

const (
	LevelHTTP     StealthLevel = 0 // No browser — HTTP only
	LevelHeadless StealthLevel = 1 // Rod headless + stealth
	LevelHeadful  StealthLevel = 2 // Rod headful + Xvfb
)

// Config configures the browser manager.
type Config struct {
	// RemoteURL is the WebSocket URL of an external Chrome instance.
	// Empty = launch a local Chrome via launcher.
	RemoteURL string

	// MemoryLimit in bytes. Recycle Chrome when exceeded. Default: 1GB.
	MemoryLimit int64

	// RecycleInterval is the maximum lifetime of a Chrome process. Default: 4h.
	RecycleInterval time.Duration

	// ResourceBlocking lists resource types to block (images, fonts, media, stylesheets).
	ResourceBlocking []string

	// Stealth sets the default stealth level. Default: LevelHeadless.
	Stealth StealthLevel

	// XvfbDisplay for headful mode. Default: ":99".
	XvfbDisplay string

	Logger *slog.Logger
}

func (c *Config) defaults() {
	if c.MemoryLimit <= 0 {
		c.MemoryLimit = 1 << 30 // 1GB
	}
	if c.RecycleInterval <= 0 {
		c.RecycleInterval = 4 * time.Hour
	}
	if c.XvfbDisplay == "" {
		c.XvfbDisplay = ":99"
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// RecycleCallback is called before and after Chrome recycling so observers
// can flush buffers and reconnect.
type RecycleCallback struct {
	// BeforeRecycle is called before Chrome is killed. Observers should flush.
	BeforeRecycle func()
	// AfterRecycle is called after Chrome is restarted. Observers should reconnect.
	AfterRecycle func(browser *rod.Browser)
}

// Manager manages Chrome lifecycle.
type Manager struct {
	cfg     Config
	mu      sync.RWMutex
	browser *rod.Browser
	lnch    *launcher.Launcher
	xvfb    *exec.Cmd
	startAt time.Time
	closed  bool
	cb      *RecycleCallback
}

// NewManager creates a browser Manager. Call Start to launch Chrome.
func NewManager(cfg Config) *Manager {
	cfg.defaults()
	return &Manager{cfg: cfg}
}

// SetRecycleCallback sets the callback for recycle events.
func (m *Manager) SetRecycleCallback(cb *RecycleCallback) {
	m.mu.Lock()
	m.cb = cb
	m.mu.Unlock()
}

// Start launches Chrome (or connects to a remote instance) and returns
// the Rod browser handle. It also starts the memory monitor goroutine.
func (m *Manager) Start(ctx context.Context) (*rod.Browser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, fmt.Errorf("browser: manager is closed")
	}

	b, err := m.launch(ctx)
	if err != nil {
		return nil, err
	}
	m.browser = b
	m.startAt = time.Now()

	go m.monitorLoop(ctx)

	return b, nil
}

// Browser returns the current Rod browser handle. Thread-safe.
func (m *Manager) Browser() *rod.Browser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.browser
}

// Recycle kills Chrome, restarts it, and calls the AfterRecycle callback.
func (m *Manager) Recycle(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("browser: manager is closed")
	}

	return m.recycleLocked(ctx)
}

// Close shuts down Chrome and Xvfb.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.cleanup()
}

func (m *Manager) launch(ctx context.Context) (*rod.Browser, error) {
	log := m.cfg.Logger

	// Start Xvfb for headful mode.
	if m.cfg.Stealth == LevelHeadful {
		if err := m.startXvfb(); err != nil {
			return nil, fmt.Errorf("browser: xvfb: %w", err)
		}
	}

	var wsURL string

	if m.cfg.RemoteURL != "" {
		wsURL = m.cfg.RemoteURL
		log.Info("browser: connecting to remote", "url", wsURL)
	} else {
		l := launcher.New()

		if m.cfg.Stealth == LevelHeadful {
			l = l.Headless(false).Env("DISPLAY", m.cfg.XvfbDisplay)
		} else {
			l = l.Headless(true)
		}

		// Anti-detection flags.
		l = l.Set("disable-blink-features", "AutomationControlled")

		u, err := l.Launch()
		if err != nil {
			return nil, fmt.Errorf("browser: launch: %w", err)
		}
		wsURL = u
		m.lnch = l
		log.Info("browser: launched local chrome", "url", wsURL, "stealth", m.cfg.Stealth)
	}

	b := rod.New().ControlURL(wsURL)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("browser: connect: %w", err)
	}

	// Ignore certificate errors for dev/testing.
	if err := b.IgnoreCertErrors(true); err != nil {
		log.Warn("browser: ignore cert errors failed", "error", err)
	}

	return b, nil
}

func (m *Manager) recycleLocked(ctx context.Context) error {
	log := m.cfg.Logger
	log.Info("browser: recycling", "uptime", time.Since(m.startAt))

	// Notify observers to flush.
	if m.cb != nil && m.cb.BeforeRecycle != nil {
		m.cb.BeforeRecycle()
	}

	// Kill old Chrome.
	if err := m.cleanup(); err != nil {
		log.Warn("browser: cleanup during recycle", "error", err)
	}

	// Restart.
	b, err := m.launch(ctx)
	if err != nil {
		return fmt.Errorf("browser: relaunch: %w", err)
	}
	m.browser = b
	m.startAt = time.Now()

	// Notify observers to reconnect.
	if m.cb != nil && m.cb.AfterRecycle != nil {
		m.cb.AfterRecycle(b)
	}

	log.Info("browser: recycled successfully")
	return nil
}

func (m *Manager) cleanup() error {
	if m.browser != nil {
		m.browser.Close()
		m.browser = nil
	}
	if m.lnch != nil {
		m.lnch.Cleanup()
		m.lnch = nil
	}
	m.stopXvfb()
	return nil
}

func (m *Manager) monitorLoop(ctx context.Context) {
	log := m.cfg.Logger
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			if m.closed || m.browser == nil {
				m.mu.RUnlock()
				return
			}
			startAt := m.startAt
			m.mu.RUnlock()

			// Check time-based recycling.
			if time.Since(startAt) > m.cfg.RecycleInterval {
				log.Info("browser: recycle interval reached")
				if err := m.Recycle(ctx); err != nil {
					log.Error("browser: recycle failed", "error", err)
				}
				continue
			}

			// Check memory-based recycling via Chrome's metrics.
			m.mu.RLock()
			b := m.browser
			m.mu.RUnlock()
			if b == nil {
				continue
			}

			metrics, err := getJSHeapUsage(b)
			if err != nil {
				log.Debug("browser: heap check failed", "error", err)
				continue
			}

			if metrics > m.cfg.MemoryLimit {
				log.Info("browser: memory limit exceeded",
					"used", metrics, "limit", m.cfg.MemoryLimit)
				if err := m.Recycle(ctx); err != nil {
					log.Error("browser: recycle failed", "error", err)
				}
			}
		}
	}
}

// getJSHeapUsage queries Chrome's JS heap via the Performance domain.
func getJSHeapUsage(b *rod.Browser) (int64, error) {
	// Use the browser-level CDP to get overall metrics.
	// This queries the first page's execution context as a proxy.
	pages, err := b.Pages()
	if err != nil || len(pages) == 0 {
		return 0, fmt.Errorf("no pages for heap check")
	}

	// Get performance metrics from the first page.
	res, err := pages[0].Eval(`() => {
		if (performance.memory) {
			return performance.memory.usedJSHeapSize;
		}
		return 0;
	}`)
	if err != nil {
		return 0, err
	}

	return int64(res.Value.Int()), nil
}
