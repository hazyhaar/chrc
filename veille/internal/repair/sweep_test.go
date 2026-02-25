package repair

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hazyhaar/chrc/veille/internal/store"
	_ "modernc.org/sqlite"
)

// mockPool implements PoolResolver for testing.
type mockPool struct {
	dbs map[string]*sql.DB
}

func (m *mockPool) Resolve(_ context.Context, dossierID string) (*sql.DB, error) {
	db, ok := m.dbs[dossierID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return db, nil
}

func TestSweepOnce_RecoverSource(t *testing.T) {
	// WHAT: Sweep probes a broken source, resets it if the URL responds 200.
	// WHY: Sources that recover should automatically re-enter the scheduler.
	db := openTestDB(t)
	st := store.NewStore(db)
	ctx := context.Background()

	// Start an HTTP server that returns 200.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer ts.Close()

	// Insert a broken source pointing to the test server.
	src := &store.Source{
		ID: "src-sweep-1", Name: "Recovering", URL: ts.URL,
		SourceType: "web", Enabled: true, FailCount: 10, LastStatus: "broken",
	}
	st.InsertSource(ctx, src)
	// Set status to broken.
	st.SetSourceStatus(ctx, src.ID, "broken")

	pool := &mockPool{dbs: map[string]*sql.DB{"d1": db}}
	lister := func(ctx context.Context) ([]string, error) {
		return []string{"d1"}, nil
	}

	sw := NewSweeper(pool, lister, nil, 0)
	results := sw.SweepOnce(ctx)

	if len(results) != 1 {
		t.Fatalf("results: got %d, want 1", len(results))
	}
	if !results[0].Recovered {
		t.Errorf("should be recovered, got error: %s", results[0].Error)
	}

	// Verify source was reset.
	got, _ := st.GetSource(ctx, "src-sweep-1")
	if got.FailCount != 0 {
		t.Errorf("fail_count: got %d, want 0", got.FailCount)
	}
	if got.LastStatus != "pending" {
		t.Errorf("status: got %q, want pending", got.LastStatus)
	}
}

func TestSweepOnce_StillBroken(t *testing.T) {
	// WHAT: Sweep leaves broken sources that still fail.
	// WHY: Don't reset sources that are genuinely broken.
	db := openTestDB(t)
	st := store.NewStore(db)
	ctx := context.Background()

	// Server returns 404.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()

	src := &store.Source{
		ID: "src-sweep-2", Name: "StillBroken", URL: ts.URL,
		SourceType: "web", Enabled: true, FailCount: 5, LastStatus: "error",
	}
	st.InsertSource(ctx, src)

	pool := &mockPool{dbs: map[string]*sql.DB{"d1": db}}
	lister := func(ctx context.Context) ([]string, error) {
		return []string{"d1"}, nil
	}

	sw := NewSweeper(pool, lister, nil, 0)
	results := sw.SweepOnce(ctx)

	if len(results) != 1 {
		t.Fatalf("results: got %d, want 1", len(results))
	}
	if results[0].Recovered {
		t.Error("should NOT be recovered")
	}

	// Verify source was NOT reset.
	got, _ := st.GetSource(ctx, "src-sweep-2")
	if got.FailCount != 5 {
		t.Errorf("fail_count should be unchanged: got %d, want 5", got.FailCount)
	}
}

func TestSweepOnce_SkipsQuestionSources(t *testing.T) {
	// WHAT: Sweep skips question-type sources.
	// WHY: question:// URLs are synthetic, not HTTP-probeable.
	db := openTestDB(t)
	st := store.NewStore(db)
	ctx := context.Background()

	src := &store.Source{
		ID: "src-sweep-q", Name: "Question", URL: "question://q1",
		SourceType: "question", Enabled: true, FailCount: 3, LastStatus: "error",
	}
	st.InsertSource(ctx, src)

	pool := &mockPool{dbs: map[string]*sql.DB{"d1": db}}
	lister := func(ctx context.Context) ([]string, error) {
		return []string{"d1"}, nil
	}

	sw := NewSweeper(pool, lister, nil, 0)
	results := sw.SweepOnce(ctx)

	if len(results) != 0 {
		t.Errorf("should skip question sources, got %d results", len(results))
	}
}

func TestSweepOnce_NoShards(t *testing.T) {
	// WHAT: Sweep with no shards returns empty.
	// WHY: Graceful no-op when no dossiers exist.
	pool := &mockPool{dbs: map[string]*sql.DB{}}
	lister := func(ctx context.Context) ([]string, error) {
		return nil, nil
	}

	sw := NewSweeper(pool, lister, nil, 0)
	results := sw.SweepOnce(context.Background())

	if len(results) != 0 {
		t.Errorf("should return 0 results, got %d", len(results))
	}
}
