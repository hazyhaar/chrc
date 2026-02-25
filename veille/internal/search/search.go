// CLAUDE:SUMMARY Search engine abstraction with strategy dispatch (api via apifetch, generic stub).
// Package search provides a search engine registry and query execution.
//
// Two strategies are supported:
//   - "api": pure HTTP JSON (e.g. Brave Search). Uses apifetch under the hood.
//   - "generic": Rod/Chrome + CSS selectors (stub — not yet wired to domwatch).
package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/apifetch"
)

// Engine describes a search engine.
type Engine struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Strategy     string         `json:"strategy"`      // "api" | "generic"
	URLTemplate  string         `json:"url_template"`   // e.g. "https://api.search.brave.com/...?q={query}"
	APIConfig    apifetch.Config `json:"api_config"`    // for strategy=api
	Selectors    Selectors      `json:"selectors"`     // for strategy=generic
	StealthLevel int            `json:"stealth_level"`
	RateLimitMs  int64          `json:"rate_limit_ms"`
	MaxPages     int            `json:"max_pages"`
	Enabled      bool           `json:"enabled"`
	CreatedAt    int64          `json:"created_at"`
	UpdatedAt    int64          `json:"updated_at"`
}

// Selectors holds CSS selectors for generic (browser-based) scraping.
type Selectors struct {
	ResultItem string `json:"result_item"`
	Title      string `json:"title"`
	Link       string `json:"link"`
	Snippet    string `json:"snippet"`
}

// Result is a single search result.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// ErrGenericNotAvailable is returned when the generic strategy is used.
var ErrGenericNotAvailable = errors.New("search: generic strategy not yet available (requires domwatch)")

// Search executes a query against the given engine and returns results.
func Search(ctx context.Context, engine *Engine, query string, client *http.Client) ([]Result, error) {
	if engine == nil {
		return nil, errors.New("search: nil engine")
	}
	if !engine.Enabled {
		return nil, nil
	}

	switch engine.Strategy {
	case "api":
		return searchAPI(ctx, engine, query, client)
	case "generic":
		return nil, ErrGenericNotAvailable
	default:
		return nil, fmt.Errorf("search: unknown strategy %q", engine.Strategy)
	}
}

// searchAPI replaces {query} in URLTemplate and calls apifetch.Fetch.
func searchAPI(ctx context.Context, engine *Engine, query string, client *http.Client) ([]Result, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	// Build URL by replacing {query} placeholder.
	searchURL := strings.ReplaceAll(engine.URLTemplate, "{query}", url.QueryEscape(query))

	apiResults, err := apifetch.Fetch(ctx, client, searchURL, engine.APIConfig)
	if err != nil {
		return nil, fmt.Errorf("search api: %w", err)
	}

	results := make([]Result, len(apiResults))
	for i, r := range apiResults {
		results[i] = Result{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Text,
		}
	}
	return results, nil
}
