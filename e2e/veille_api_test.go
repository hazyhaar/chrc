package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hazyhaar/chrc/veille"
	"github.com/hazyhaar/pkg/connectivity"
)

func TestE2E_API_FullCycle(t *testing.T) {
	// WHAT: API source → fetch → parse → extractions + .md in buffer.
	// WHY: End-to-end validation of the API pipeline.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [
				{
					"title": "Open Data Initiative",
					"description": "A comprehensive open data platform providing access to government datasets, including environmental monitoring, transportation, and public health statistics.",
					"page": "https://opendata.example.com/initiative"
				},
				{
					"title": "Machine Learning for Climate",
					"description": "New research applying machine learning techniques to climate modeling, achieving significant improvements in precipitation and temperature forecasting accuracy.",
					"page": "https://opendata.example.com/ml-climate"
				}
			]
		}`))
	}))
	defer srv.Close()

	pool := newTestPool()
	defer pool.Close()

	bufDir := filepath.Join(t.TempDir(), "buffer", "pending")
	cfg := &veille.Config{}
	cfg.BufferDir = bufDir

	// Create connectivity router with api_fetch service.
	router := connectivity.New()
	router.RegisterLocal("api_fetch", veille.NewAPIService())

	svc, err := veille.New(pool, cfg, nil,
		veille.WithURLValidator(func(_ string) error { return nil }),
		veille.WithRouter(router),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()

	// Add API source.
	src := &veille.Source{
		Name:       "E2E API Source",
		URL:        srv.URL,
		SourceType: "api",
		Enabled:    true,
		ConfigJSON: `{"result_path":"data","fields":{"title":"title","text":"description","url":"page"}}`,
	}
	if err := svc.AddSource(ctx, "d1", src); err != nil {
		t.Fatalf("add source: %v", err)
	}

	// Trigger fetch.
	if err := svc.FetchNow(ctx, "d1", src.ID); err != nil {
		t.Fatalf("fetch now: %v", err)
	}

	// Verify extractions.
	exts, _ := svc.ListExtractions(ctx, "d1", src.ID, 10)
	if len(exts) != 2 {
		t.Fatalf("extractions: got %d, want 2", len(exts))
	}

	// Verify .md files in buffer.
	entries, _ := os.ReadDir(bufDir)
	mdCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			mdCount++
		}
	}
	if mdCount != 2 {
		t.Errorf("buffer .md files: got %d, want 2", mdCount)
	}

	// Verify dedup.
	svc.FetchNow(ctx, "d1", src.ID)
	exts2, _ := svc.ListExtractions(ctx, "d1", src.ID, 10)
	if len(exts2) != 2 {
		t.Errorf("after dedup: got %d extractions, want 2", len(exts2))
	}

	// Verify search.
	results, _ := svc.Search(ctx, "d1", "machine learning", 10)
	if len(results) == 0 {
		t.Error("search should find 'machine learning'")
	}
}
