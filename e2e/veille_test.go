package e2e

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hazyhaar/chrc/veille"

	_ "modernc.org/sqlite"
)

type testPool struct {
	dbs map[string]*sql.DB
}

func newTestPool() *testPool {
	return &testPool{dbs: make(map[string]*sql.DB)}
}

func (tp *testPool) Resolve(_ context.Context, dossierID string) (*sql.DB, error) {
	if db, ok := tp.dbs[dossierID]; ok {
		return db, nil
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	veille.ApplySchema(db)
	tp.dbs[dossierID] = db
	return db, nil
}

func (tp *testPool) Close() {
	for _, db := range tp.dbs {
		db.Close()
	}
}

func TestE2E_FullCycle(t *testing.T) {
	// WHAT: Full cycle: add source → fetch → search.
	// WHY: End-to-end validation of the entire pipeline.
	htmlContent := `<!DOCTYPE html><html><head><title>E2E Test</title></head>
	<body><main><article>
	<h1>Artificial Intelligence Advances</h1>
	<p>Recent breakthroughs in machine learning have transformed how we approach
	natural language processing and computer vision tasks. Deep neural networks
	continue to push the boundaries of what is possible with automated reasoning
	and pattern recognition systems.</p>
	</article></main></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(htmlContent))
	}))
	defer srv.Close()

	pool := newTestPool()
	defer pool.Close()
	svc, err := veille.New(pool, nil, nil, veille.WithURLValidator(func(_ string) error { return nil }))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx := context.Background()

	// Add a source.
	src := &veille.Source{Name: "AI News", URL: srv.URL, Enabled: true}
	if err := svc.AddSource(ctx, "d1", src); err != nil {
		t.Fatalf("add source: %v", err)
	}

	// Trigger fetch.
	if err := svc.FetchNow(ctx, "d1", src.ID); err != nil {
		t.Fatalf("fetch now: %v", err)
	}

	// Verify extraction.
	exts, err := svc.ListExtractions(ctx, "d1", src.ID, 10)
	if err != nil {
		t.Fatalf("list extractions: %v", err)
	}
	if len(exts) == 0 {
		t.Fatal("no extractions after fetch")
	}

	// Search.
	results, err := svc.Search(ctx, "d1", "machine learning", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("search should find results for 'machine learning'")
	}

	// Stats.
	stats, err := svc.Stats(ctx, "d1")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Sources != 1 || stats.Extractions != 1 {
		t.Errorf("stats: sources=%d, extractions=%d", stats.Sources, stats.Extractions)
	}

	// Fetch history.
	history, err := svc.FetchHistory(ctx, "d1", src.ID, 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("no fetch history")
	}
	if history[0].Status != "ok" {
		t.Errorf("fetch status: %s", history[0].Status)
	}
}

func TestE2E_MultiTenantIsolation(t *testing.T) {
	// WHAT: Different dossiers are isolated.
	// WHY: Multi-tenancy is the architectural foundation.
	htmlA := `<html><body><main><p>Content for dossier A about quantum computing research and applications.</p></main></body></html>`
	htmlB := `<html><body><main><p>Content for dossier B about blockchain and distributed ledger technology.</p></main></body></html>`

	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(htmlA))
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(htmlB))
	}))
	defer srvB.Close()

	pool := newTestPool()
	defer pool.Close()
	svc, _ := veille.New(pool, nil, nil, veille.WithURLValidator(func(_ string) error { return nil }))
	ctx := context.Background()

	// Dossier A adds a source.
	srcA := &veille.Source{Name: "A Source", URL: srvA.URL, Enabled: true}
	svc.AddSource(ctx, "dossier-A", srcA)
	svc.FetchNow(ctx, "dossier-A", srcA.ID)

	// Dossier B adds a source.
	srcB := &veille.Source{Name: "B Source", URL: srvB.URL, Enabled: true}
	svc.AddSource(ctx, "dossier-B", srcB)
	svc.FetchNow(ctx, "dossier-B", srcB.ID)

	// Dossier A should not see dossier B's data.
	statsA, _ := svc.Stats(ctx, "dossier-A")
	statsB, _ := svc.Stats(ctx, "dossier-B")

	if statsA.Sources != 1 {
		t.Errorf("dossier A sources: got %d, want 1", statsA.Sources)
	}
	if statsB.Sources != 1 {
		t.Errorf("dossier B sources: got %d, want 1", statsB.Sources)
	}

	// Search in A should only find A's content.
	resultsA, _ := svc.Search(ctx, "dossier-A", "quantum", 10)
	if len(resultsA) == 0 {
		t.Error("dossier A should find 'quantum'")
	}

	resultsB, _ := svc.Search(ctx, "dossier-B", "blockchain", 10)
	if len(resultsB) == 0 {
		t.Error("dossier B should find 'blockchain'")
	}

	// Cross-check: A should NOT find B's content.
	crossA, _ := svc.Search(ctx, "dossier-A", "blockchain", 10)
	if len(crossA) > 0 {
		t.Error("dossier A should NOT find 'blockchain' (dossier B's data)")
	}
}

func TestE2E_DuplicateSourceRejected(t *testing.T) {
	// WHAT: Adding the same source URL twice returns ErrDuplicateSource.
	// WHY: Dedup prevents wasted network resources and duplicate results.
	pool := newTestPool()
	defer pool.Close()
	svc, _ := veille.New(pool, nil, nil, veille.WithURLValidator(func(_ string) error { return nil }))
	ctx := context.Background()

	src1 := &veille.Source{Name: "First", URL: "https://example.com/feed", SourceType: "rss", FetchInterval: 3600000, Enabled: true}
	if err := svc.AddSource(ctx, "d1", src1); err != nil {
		t.Fatalf("first add: %v", err)
	}

	// Same URL — must fail.
	src2 := &veille.Source{Name: "Duplicate", URL: "https://example.com/feed", SourceType: "rss", FetchInterval: 3600000, Enabled: true}
	err := svc.AddSource(ctx, "d1", src2)
	if err == nil {
		t.Fatal("expected error for duplicate URL, got nil")
	}
	if !errors.Is(err, veille.ErrDuplicateSource) {
		t.Errorf("expected ErrDuplicateSource, got: %v", err)
	}

	// Normalized variant — must also fail.
	src3 := &veille.Source{Name: "Variant", URL: "HTTPS://Example.COM/feed/", SourceType: "rss", FetchInterval: 3600000, Enabled: true}
	err = svc.AddSource(ctx, "d1", src3)
	if !errors.Is(err, veille.ErrDuplicateSource) {
		t.Errorf("normalized variant should be duplicate, got: %v", err)
	}

	// Different URL — must succeed.
	src4 := &veille.Source{Name: "Different", URL: "https://example.com/other", SourceType: "web", FetchInterval: 3600000, Enabled: true}
	if err := svc.AddSource(ctx, "d1", src4); err != nil {
		t.Errorf("different URL should succeed, got: %v", err)
	}
}

func TestE2E_InvalidInput_Rejected(t *testing.T) {
	// WHAT: AddSource rejects invalid inputs (DoS fetch_interval, unknown type).
	// WHY: Input validation prevents DoS and unpredictable pipeline behavior.
	pool := newTestPool()
	defer pool.Close()
	svc, _ := veille.New(pool, nil, nil, veille.WithURLValidator(func(_ string) error { return nil }))
	ctx := context.Background()

	cases := []struct {
		name string
		src  veille.Source
	}{
		{"low_interval", veille.Source{Name: "DoS", URL: "https://example.com", SourceType: "web", FetchInterval: 100}},
		{"unknown_type", veille.Source{Name: "Evil", URL: "https://example.com", SourceType: "evil", FetchInterval: 3600000}},
		{"empty_name", veille.Source{Name: "", URL: "https://example.com", SourceType: "web", FetchInterval: 3600000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.AddSource(ctx, "d1", &tc.src)
			if !errors.Is(err, veille.ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput, got: %v", err)
			}
		})
	}
}
