// Package fetcher implements the HTTP-only acquisition path (stealth level 0).
// No browser, no JS — a single HTTP GET that produces a Snapshot.
// Covers ~90% of static sites.
package fetcher

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/hazyhaar/pkg/domwatch/mutation"
	"github.com/hazyhaar/pkg/idgen"
)

// Result is the outcome of an HTTP fetch.
type Result struct {
	Snapshot  mutation.Snapshot
	Sufficient bool   // true if the HTML has enough content (no escalation needed)
	StatusCode int
	ETag       string
	LastMod    string
}

// Fetcher performs HTTP GETs and produces Snapshots.
type Fetcher struct {
	client *http.Client
	ua     string
	logger *slog.Logger
}

// Option configures a Fetcher.
type Option func(*Fetcher)

// WithClient sets a custom HTTP client.
func WithClient(c *http.Client) Option {
	return func(f *Fetcher) { f.client = c }
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(f *Fetcher) { f.ua = ua }
}

// WithLogger sets a custom logger.
func WithLogger(l *slog.Logger) Option {
	return func(f *Fetcher) { f.logger = l }
}

// New creates a Fetcher with sensible defaults.
func New(opts ...Option) *Fetcher {
	f := &Fetcher{
		client: &http.Client{Timeout: 30 * time.Second},
		ua:     "Mozilla/5.0 (compatible; DOMWatch/1.0)",
		logger: slog.Default(),
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Fetch GETs a URL and returns the result with a Snapshot and sufficiency signal.
func (f *Fetcher) Fetch(ctx context.Context, pageURL, pageID string) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetcher: new request: %w", err)
	}
	req.Header.Set("User-Agent", f.ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetcher: do: %w", err)
	}
	defer resp.Body.Close()

	// Cap read to 10MB to prevent runaway downloads.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("fetcher: read body: %w", err)
	}

	now := time.Now().UnixMilli()
	hash := mutation.HashHTML(body)

	res := &Result{
		Snapshot: mutation.Snapshot{
			ID:        idgen.New(),
			PageURL:   pageURL,
			PageID:    pageID,
			HTML:      body,
			HTMLHash:  hash,
			Timestamp: now,
		},
		StatusCode: resp.StatusCode,
		ETag:       resp.Header.Get("ETag"),
		LastMod:    resp.Header.Get("Last-Modified"),
		Sufficient: IsSufficient(body),
	}

	f.logger.Debug("fetcher: fetched",
		"url", pageURL, "status", resp.StatusCode,
		"size", len(body), "sufficient", res.Sufficient)

	return res, nil
}

// Head performs a HEAD request to check ETag/Last-Modified without downloading.
func (f *Fetcher) Head(ctx context.Context, pageURL string) (etag, lastMod string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, pageURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("fetcher: head request: %w", err)
	}
	req.Header.Set("User-Agent", f.ua)

	resp, err := f.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetcher: head do: %w", err)
	}
	resp.Body.Close()

	return resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), nil
}
