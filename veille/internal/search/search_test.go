package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hazyhaar/chrc/veille/internal/apifetch"
)

func TestSearch_APIStrategy(t *testing.T) {
	// WHAT: API strategy fetches JSON and returns mapped results.
	// WHY: Core search path for Brave Search and similar APIs.
	apiResp := map[string]any{
		"web": map[string]any{
			"results": []map[string]any{
				{"title": "Go Programming", "url": "https://go.dev", "description": "The Go language"},
				{"title": "Rust Lang", "url": "https://rust-lang.org", "description": "Rust programming"},
			},
		},
	}
	body, _ := json.Marshal(apiResp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify query was substituted.
		if !strings.Contains(r.URL.RawQuery, "golang") {
			t.Errorf("query not found in URL: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	engine := &Engine{
		ID:          "brave",
		Name:        "Brave Search",
		Strategy:    "api",
		URLTemplate: srv.URL + "?q={query}",
		APIConfig: apifetch.Config{
			ResultPath: "web.results",
			Fields:     map[string]string{"title": "title", "text": "description", "url": "url"},
		},
		Enabled: true,
	}

	results, err := Search(context.Background(), engine, "golang", nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("count: got %d, want 2", len(results))
	}
	if results[0].Title != "Go Programming" {
		t.Errorf("title: got %q", results[0].Title)
	}
	if results[0].URL != "https://go.dev" {
		t.Errorf("url: got %q", results[0].URL)
	}
	if results[0].Snippet != "The Go language" {
		t.Errorf("snippet: got %q", results[0].Snippet)
	}
}

func TestSearch_GenericStub(t *testing.T) {
	// WHAT: Generic strategy returns not-available error.
	// WHY: Generic requires domwatch which is not yet wired.
	engine := &Engine{
		ID:       "ddg",
		Name:     "DuckDuckGo",
		Strategy: "generic",
		Enabled:  true,
	}

	_, err := Search(context.Background(), engine, "test", nil)
	if err != ErrGenericNotAvailable {
		t.Errorf("expected ErrGenericNotAvailable, got: %v", err)
	}
}

func TestSearch_DisabledSkipped(t *testing.T) {
	// WHAT: Disabled engines return nil results without error.
	// WHY: Disabled engines should be silently skipped.
	engine := &Engine{
		ID:       "disabled",
		Name:     "Disabled",
		Strategy: "api",
		Enabled:  false,
	}

	results, err := Search(context.Background(), engine, "test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for disabled engine, got %d", len(results))
	}
}

func TestSearch_URLTemplateExpansion(t *testing.T) {
	// WHAT: {query} in URL template is replaced with URL-encoded query.
	// WHY: Queries with special characters must be properly encoded.
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	engine := &Engine{
		ID:          "test",
		Name:        "Test",
		Strategy:    "api",
		URLTemplate: srv.URL + "/search?q={query}&format=json",
		Enabled:     true,
	}

	Search(context.Background(), engine, "hello world", nil)

	if !strings.Contains(gotURL, "hello+world") && !strings.Contains(gotURL, "hello%20world") {
		t.Errorf("query not properly encoded in URL: %s", gotURL)
	}
}
