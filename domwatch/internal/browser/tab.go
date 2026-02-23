package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
)

// Tab wraps a Rod page with domwatch-specific setup: stealth, resource
// blocking, and DOM tracking initialisation.
type Tab struct {
	Page     *rod.Page
	PageURL  string
	PageID   string
	Stealth  StealthLevel
	manager  *Manager
}

// OpenTab creates a new tab, navigates to the URL with stealth applied,
// and enables DOM domain tracking.
func OpenTab(ctx context.Context, mgr *Manager, pageURL, pageID string, level StealthLevel) (*Tab, error) {
	b := mgr.Browser()
	if b == nil {
		return nil, fmt.Errorf("browser: no active browser")
	}

	var page *rod.Page
	var err error

	if level >= LevelHeadless {
		page, err = stealth.Page(b)
	} else {
		page, err = b.Page(proto.TargetCreateTarget{URL: ""})
	}
	if err != nil {
		return nil, fmt.Errorf("browser: create tab: %w", err)
	}

	// Apply resource blocking.
	if len(mgr.cfg.ResourceBlocking) > 0 {
		if err := applyResourceBlocking(page, mgr.cfg.ResourceBlocking); err != nil {
			mgr.cfg.Logger.Warn("browser: resource blocking failed", "error", err)
		}
	}

	// Navigate with timeout.
	navCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err = page.Context(navCtx).Navigate(pageURL)
	if err != nil {
		page.Close()
		return nil, fmt.Errorf("browser: navigate %s: %w", pageURL, err)
	}

	// Wait for page load.
	if err := page.Context(navCtx).WaitLoad(); err != nil {
		mgr.cfg.Logger.Warn("browser: wait load timeout", "url", pageURL, "error", err)
	}

	return &Tab{
		Page:    page,
		PageURL: pageURL,
		PageID:  pageID,
		Stealth: level,
		manager: mgr,
	}, nil
}

// GetFullDOM serialises the complete DOM as outer HTML.
func (t *Tab) GetFullDOM(ctx context.Context) ([]byte, error) {
	res, err := t.Page.Context(ctx).Eval(`() => document.documentElement.outerHTML`)
	if err != nil {
		return nil, fmt.Errorf("browser: get DOM: %w", err)
	}
	return []byte(res.Value.Str()), nil
}

// EnableDOMTracking calls DOM.getDocument with depth=-1 to make all nodes
// trackable by CDP. This is a critical constraint: without it, mutations
// on deep nodes are silently ignored.
func (t *Tab) EnableDOMTracking(ctx context.Context) error {
	_, err := t.Page.Context(ctx).Eval(`() => {
		// Force layout to ensure all nodes are attached.
		document.documentElement.offsetHeight;
	}`)
	return err
}

// Close closes the tab.
func (t *Tab) Close() error {
	if t.Page != nil {
		return t.Page.Close()
	}
	return nil
}
