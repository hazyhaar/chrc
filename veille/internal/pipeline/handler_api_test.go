package pipeline

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/store"
)

func TestAPI_CreatesExtractions(t *testing.T) {
	// WHAT: API handler creates extractions from JSON results.
	// WHY: API sources provide structured data that needs extraction.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": {
				"results": [
					{"name": "Result A", "body": "Detailed content of result A for processing", "link": "https://example.com/a"},
					{"name": "Result B", "body": "Detailed content of result B for processing", "link": "https://example.com/b"}
				]
			}
		}`))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{
		ID: "src-api", Name: "API Test", URL: srv.URL,
		SourceType: "api", Enabled: true,
		ConfigJSON: `{"result_path":"data.results","fields":{"title":"name","text":"body","url":"link"}}`,
	})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)
	p.RegisterHandler("api", NewAPIHandler()) // API handler is now a connectivity service; register manually for legacy tests.

	err := p.HandleJob(ctx, s, &Job{DossierID: "u_sp", SourceID: "src-api", URL: srv.URL})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	exts, _ := s.ListExtractions(ctx, "src-api", 10)
	if len(exts) != 2 {
		t.Fatalf("extractions: got %d, want 2", len(exts))
	}
}

func TestAPI_Dedup(t *testing.T) {
	// WHAT: Second API fetch with same results doesn't duplicate extractions.
	// WHY: API endpoints often return the same data on consecutive calls.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"title":"Stable","text":"Stable content that repeats","url":"https://example.com/s"}]`))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{
		ID: "src-adup", Name: "API Dedup", URL: srv.URL,
		SourceType: "api", Enabled: true,
	})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)
	p.RegisterHandler("api", NewAPIHandler())
	job := &Job{DossierID: "u_sp", SourceID: "src-adup", URL: srv.URL}

	p.HandleJob(ctx, s, job)
	p.HandleJob(ctx, s, job)

	exts, _ := s.ListExtractions(ctx, "src-adup", 10)
	if len(exts) != 1 {
		t.Errorf("extractions: got %d, want 1 (dedup)", len(exts))
	}
}

func TestAPI_WritesBuffer(t *testing.T) {
	// WHAT: API handler writes .md files to buffer.
	// WHY: API results need to reach the RAG island via buffer.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"title":"Buffered","text":"Content for buffer test","url":"https://buf.com"}]`))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{
		ID: "src-abuf", Name: "API Buf", URL: srv.URL,
		SourceType: "api", Enabled: true,
	})

	bufDir := filepath.Join(t.TempDir(), "pending")
	f := fetch.New(fetch.Config{})
	p := New(f, nil)
	p.RegisterHandler("api", NewAPIHandler())
	p.SetBuffer(buffer.NewWriter(bufDir))

	err := p.HandleJob(ctx, s, &Job{DossierID: "u_sp", SourceID: "src-abuf", URL: srv.URL})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	entries, _ := os.ReadDir(bufDir)
	mdCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" {
			mdCount++
		}
	}
	if mdCount != 1 {
		t.Errorf("buffer .md files: got %d, want 1", mdCount)
	}
}
