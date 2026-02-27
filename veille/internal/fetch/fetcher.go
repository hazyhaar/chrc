// CLAUDE:SUMMARY HTTP conditional GET fetcher with ETag, If-Modified-Since, and content-hash dedup.
// Package fetch implements HTTP content fetching with conditional GET support.
//
// Supports ETag, If-Modified-Since, and content-hash-based change detection.
package fetch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hazyhaar/pkg/horosafe"
)

// Result contains the outcome of a fetch.
type Result struct {
	Body       []byte
	StatusCode int
	Hash       string // SHA-256 of body
	ETag       string // from response header
	LastMod    string // from response header
	Changed    bool   // true if content is new/different
}

// Config configures the fetcher.
type Config struct {
	Timeout  time.Duration // HTTP timeout. Default: 30s.
	MaxBytes int64         // Max response body size. Default: 10MB.
	// UserAgent sent with requests.
	UserAgent string
	// URLValidator validates URLs before fetch (SSRF prevention).
	// Default: horosafe.ValidateURL.
	URLValidator func(string) error
}

func (c *Config) defaults() {
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxBytes <= 0 {
		c.MaxBytes = 10 * 1024 * 1024 // 10MB
	}
	if c.UserAgent == "" {
		c.UserAgent = "chrc-veille/1.0"
	}
	if c.URLValidator == nil {
		c.URLValidator = horosafe.ValidateURL
	}
}

// Fetcher performs HTTP requests with conditional GET.
type Fetcher struct {
	client *http.Client
	config Config
}

// New creates a Fetcher with SSRF protection on redirects.
func New(cfg Config) *Fetcher {
	cfg.defaults()
	validate := cfg.URLValidator
	return &Fetcher{
		client: &http.Client{
			Timeout: cfg.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects (%d)", len(via))
				}
				if err := validate(req.URL.String()); err != nil {
					return fmt.Errorf("redirect blocked (SSRF): %w", err)
				}
				return nil
			},
		},
		config: cfg,
	}
}

// Fetch retrieves a URL. If etag or lastMod are provided, sends conditional headers.
// Returns Changed=false on 304 Not Modified.
// If prevHash is provided and body hash matches, also returns Changed=false.
func (f *Fetcher) Fetch(ctx context.Context, url, etag, lastMod, prevHash string) (*Result, error) {
	// SSRF: validate URL before request.
	if err := f.config.URLValidator(url); err != nil {
		return nil, fmt.Errorf("URL blocked (SSRF): %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", f.config.UserAgent)

	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastMod != "" {
		req.Header.Set("If-Modified-Since", lastMod)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return &Result{
			StatusCode: 304,
			Changed:    false,
			ETag:       resp.Header.Get("ETag"),
			LastMod:    resp.Header.Get("Last-Modified"),
		}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return &Result{StatusCode: resp.StatusCode}, fmt.Errorf("http %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.config.MaxBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	h := sha256.Sum256(body)
	hash := fmt.Sprintf("%x", h)

	changed := prevHash == "" || hash != prevHash
	return &Result{
		Body:       body,
		StatusCode: resp.StatusCode,
		Hash:       hash,
		ETag:       resp.Header.Get("ETag"),
		LastMod:    resp.Header.Get("Last-Modified"),
		Changed:    changed,
	}, nil
}
