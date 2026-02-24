package e2e

import (
	"context"
	"database/sql"
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

func (tp *testPool) Resolve(_ context.Context, userID, spaceID string) (*sql.DB, error) {
	key := userID + "/" + spaceID
	if db, ok := tp.dbs[key]; ok {
		return db, nil
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	veille.ApplySchema(db)
	tp.dbs[key] = db
	return db, nil
}

func (tp *testPool) Close() {
	for _, db := range tp.dbs {
		db.Close()
	}
}

type testSpaces struct {
	spaces []veille.SpaceInfo
}

func (ts *testSpaces) CreateSpace(_ context.Context, userID, spaceID, name string) error {
	ts.spaces = append(ts.spaces, veille.SpaceInfo{UserID: userID, SpaceID: spaceID, Name: name})
	return nil
}

func (ts *testSpaces) DeleteSpace(_ context.Context, userID, spaceID string) error {
	for i, s := range ts.spaces {
		if s.UserID == userID && s.SpaceID == spaceID {
			ts.spaces = append(ts.spaces[:i], ts.spaces[i+1:]...)
			return nil
		}
	}
	return nil
}

func (ts *testSpaces) ListSpaces(_ context.Context, userID string) ([]veille.SpaceInfo, error) {
	var result []veille.SpaceInfo
	for _, s := range ts.spaces {
		if s.UserID == userID {
			result = append(result, s)
		}
	}
	return result, nil
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
	spaces := &testSpaces{}
	svc, err := veille.New(pool, spaces, nil, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx := context.Background()

	// Add a source.
	src := &veille.Source{Name: "AI News", URL: srv.URL, Enabled: true}
	if err := svc.AddSource(ctx, "user-1", "tech-watch", src); err != nil {
		t.Fatalf("add source: %v", err)
	}

	// Trigger fetch.
	if err := svc.FetchNow(ctx, "user-1", "tech-watch", src.ID); err != nil {
		t.Fatalf("fetch now: %v", err)
	}

	// Verify extraction.
	exts, err := svc.ListExtractions(ctx, "user-1", "tech-watch", src.ID, 10)
	if err != nil {
		t.Fatalf("list extractions: %v", err)
	}
	if len(exts) == 0 {
		t.Fatal("no extractions after fetch")
	}

	// Verify chunks.
	chunks, err := svc.ListChunks(ctx, "user-1", "tech-watch", 100, 0)
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks after fetch")
	}

	// Search.
	results, err := svc.Search(ctx, "user-1", "tech-watch", "machine learning", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("search should find results for 'machine learning'")
	}

	// Stats.
	stats, err := svc.Stats(ctx, "user-1", "tech-watch")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Sources != 1 || stats.Extractions != 1 || stats.Chunks == 0 {
		t.Errorf("stats: sources=%d, extractions=%d, chunks=%d", stats.Sources, stats.Extractions, stats.Chunks)
	}

	// Fetch history.
	history, err := svc.FetchHistory(ctx, "user-1", "tech-watch", src.ID, 10)
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
	// WHAT: Different user×space combos are isolated.
	// WHY: Multi-tenancy is the architectural foundation.
	htmlA := `<html><body><main><p>Content for user A about quantum computing research and applications.</p></main></body></html>`
	htmlB := `<html><body><main><p>Content for user B about blockchain and distributed ledger technology.</p></main></body></html>`

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
	spaces := &testSpaces{}
	svc, _ := veille.New(pool, spaces, nil, nil)
	ctx := context.Background()

	// User A adds a source.
	srcA := &veille.Source{Name: "A Source", URL: srvA.URL, Enabled: true}
	svc.AddSource(ctx, "user-A", "space-A", srcA)
	svc.FetchNow(ctx, "user-A", "space-A", srcA.ID)

	// User B adds a source.
	srcB := &veille.Source{Name: "B Source", URL: srvB.URL, Enabled: true}
	svc.AddSource(ctx, "user-B", "space-B", srcB)
	svc.FetchNow(ctx, "user-B", "space-B", srcB.ID)

	// User A should not see user B's data.
	statsA, _ := svc.Stats(ctx, "user-A", "space-A")
	statsB, _ := svc.Stats(ctx, "user-B", "space-B")

	if statsA.Sources != 1 {
		t.Errorf("user A sources: got %d, want 1", statsA.Sources)
	}
	if statsB.Sources != 1 {
		t.Errorf("user B sources: got %d, want 1", statsB.Sources)
	}

	// Search in A should only find A's content.
	resultsA, _ := svc.Search(ctx, "user-A", "space-A", "quantum", 10)
	if len(resultsA) == 0 {
		t.Error("user A should find 'quantum'")
	}

	resultsB, _ := svc.Search(ctx, "user-B", "space-B", "blockchain", 10)
	if len(resultsB) == 0 {
		t.Error("user B should find 'blockchain'")
	}

	// Cross-check: A should NOT find B's content.
	crossA, _ := svc.Search(ctx, "user-A", "space-A", "blockchain", 10)
	if len(crossA) > 0 {
		t.Error("user A should NOT find 'blockchain' (user B's data)")
	}
}

func TestE2E_SpaceLifecycle(t *testing.T) {
	// WHAT: Create space, add source, delete space.
	// WHY: Full space lifecycle validation.
	pool := newTestPool()
	defer pool.Close()
	spaces := &testSpaces{}
	svc, _ := veille.New(pool, spaces, nil, nil)
	ctx := context.Background()

	// Create space.
	space, err := svc.CreateSpace(ctx, "user-1", "Legal Watch")
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	// List spaces.
	listed, err := svc.ListSpaces(ctx, "user-1")
	if err != nil {
		t.Fatalf("list spaces: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("count: got %d", len(listed))
	}

	// Delete space.
	if err := svc.DeleteSpace(ctx, "user-1", space.SpaceID); err != nil {
		t.Fatalf("delete space: %v", err)
	}

	after, _ := svc.ListSpaces(ctx, "user-1")
	if len(after) != 0 {
		t.Errorf("after delete: got %d spaces", len(after))
	}
}
