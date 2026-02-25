package apifetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetch_Simple(t *testing.T) {
	// WHAT: Fetch a simple JSON array API.
	// WHY: Simplest API pattern — root is the result array.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"title": "Item 1", "text": "Description 1", "url": "https://example.com/1"},
			{"title": "Item 2", "text": "Description 2", "url": "https://example.com/2"}
		]`))
	}))
	defer srv.Close()

	cfg := Config{}
	results, err := Fetch(context.Background(), srv.Client(), srv.URL, cfg)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results: got %d, want 2", len(results))
	}
	if results[0].Title != "Item 1" {
		t.Errorf("title: got %q", results[0].Title)
	}
	if results[0].URL != "https://example.com/1" {
		t.Errorf("url: got %q", results[0].URL)
	}
}

func TestFetch_NestedPath(t *testing.T) {
	// WHAT: Walk a dot-notation path to find results.
	// WHY: Most real APIs nest results under keys like "data.results".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"meta": {"count": 1},
			"web": {
				"results": [
					{"name": "Found It", "description": "Found via search", "link": "https://found.com"}
				]
			}
		}`))
	}))
	defer srv.Close()

	cfg := Config{
		ResultPath: "web.results",
		Fields:     map[string]string{"title": "name", "text": "description", "url": "link"},
	}
	results, err := Fetch(context.Background(), srv.Client(), srv.URL, cfg)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results: got %d, want 1", len(results))
	}
	if results[0].Title != "Found It" {
		t.Errorf("title: got %q", results[0].Title)
	}
	if results[0].Text != "Found via search" {
		t.Errorf("text: got %q", results[0].Text)
	}
}

func TestFetch_EnvExpansion(t *testing.T) {
	// WHAT: Headers with ${ENV_VAR} are expanded.
	// WHY: API keys must never be stored in SQLite.
	t.Setenv("TEST_API_KEY", "secret-key-123")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	cfg := Config{
		Headers: map[string]string{"Authorization": "Bearer ${TEST_API_KEY}"},
	}
	_, err := Fetch(context.Background(), srv.Client(), srv.URL, cfg)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotAuth != "Bearer secret-key-123" {
		t.Errorf("auth header: got %q", gotAuth)
	}
}

func TestFetch_HTTPError(t *testing.T) {
	// WHAT: Non-2xx responses return errors.
	// WHY: API errors must be propagated to the pipeline.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer srv.Close()

	_, err := Fetch(context.Background(), srv.Client(), srv.URL, Config{})
	if err == nil {
		t.Error("expected error for 403")
	}
}

func TestFetch_InvalidPath(t *testing.T) {
	// WHAT: Missing result_path key returns an error.
	// WHY: Config errors should be explicit, not silently empty.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data": {"items": []}}`))
	}))
	defer srv.Close()

	cfg := Config{ResultPath: "data.results"}
	_, err := Fetch(context.Background(), srv.Client(), srv.URL, cfg)
	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestWalkPath_Root(t *testing.T) {
	// WHAT: Empty path returns root array.
	// WHY: Some APIs return a bare array.
	items, err := walkPath([]any{"a", "b"}, "")
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("items: got %d", len(items))
	}
}

func TestWalkPath_Deep(t *testing.T) {
	// WHAT: Multi-level path resolves correctly.
	// WHY: APIs like Brave Search nest results deeply.
	data := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": []any{"x", "y"},
			},
		},
	}
	items, err := walkPath(data, "a.b.c")
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("items: got %d", len(items))
	}
}
