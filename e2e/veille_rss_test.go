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
)

func TestE2E_RSS_FullCycle(t *testing.T) {
	// WHAT: RSS source → fetch → parse → extractions + .md in buffer.
	// WHY: End-to-end validation of the RSS pipeline.
	rssXML := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>E2E Tech Feed</title>
    <link>https://feed.example.com</link>
    <item>
      <guid>e2e-rss-001</guid>
      <title>Distributed Systems in Practice</title>
      <link>https://feed.example.com/distributed</link>
      <description>An in-depth look at distributed systems patterns, consensus algorithms, and fault tolerance strategies used in modern cloud infrastructure.</description>
    </item>
    <item>
      <guid>e2e-rss-002</guid>
      <title>WebAssembly Beyond the Browser</title>
      <link>https://feed.example.com/wasm</link>
      <description>WebAssembly is breaking out of the browser sandbox. Server-side WASM runtimes are enabling portable, sandboxed execution across cloud and edge environments.</description>
    </item>
  </channel>
</rss>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(rssXML))
	}))
	defer srv.Close()

	pool := newTestPool()
	defer pool.Close()

	// Configure buffer.
	bufDir := filepath.Join(t.TempDir(), "buffer", "pending")

	cfg := &veille.Config{}
	cfg.BufferDir = bufDir

	svc, err := veille.New(pool, cfg, nil, veille.WithURLValidator(func(_ string) error { return nil }))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()

	// Add RSS source.
	src := &veille.Source{
		Name:       "E2E RSS Feed",
		URL:        srv.URL,
		SourceType: "rss",
		Enabled:    true,
	}
	if err := svc.AddSource(ctx, "d1", src); err != nil {
		t.Fatalf("add source: %v", err)
	}

	// Trigger fetch.
	if err := svc.FetchNow(ctx, "d1", src.ID); err != nil {
		t.Fatalf("fetch now: %v", err)
	}

	// Verify extractions (one per RSS entry).
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

	// Verify dedup: second fetch should not create new extractions.
	svc.FetchNow(ctx, "d1", src.ID)
	exts2, _ := svc.ListExtractions(ctx, "d1", src.ID, 10)
	if len(exts2) != 2 {
		t.Errorf("after second fetch: got %d extractions, want 2 (dedup)", len(exts2))
	}

	// Verify search finds RSS content.
	results, _ := svc.Search(ctx, "d1", "distributed systems", 10)
	if len(results) == 0 {
		t.Error("search should find 'distributed systems'")
	}

	// Verify fetch history.
	history, _ := svc.FetchHistory(ctx, "d1", src.ID, 10)
	if len(history) < 2 {
		t.Errorf("fetch history: got %d, want >= 2", len(history))
	}
}
