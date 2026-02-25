package scheduler

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/store"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	if err := store.ApplySchema(db); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return db
}

func TestEnqueueDueSources(t *testing.T) {
	// WHAT: Scheduler finds and enqueues due sources.
	// WHY: Core scheduling loop.
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	s := store.NewStore(db)
	past := time.Now().UnixMilli() - 7200000
	s.InsertSource(ctx, &store.Source{ID: "src-due", Name: "Due", URL: "https://due.com", Enabled: true, FetchInterval: 3600000, LastFetchedAt: &past})
	s.InsertSource(ctx, &store.Source{ID: "src-new", Name: "New", URL: "https://new.com", Enabled: true})

	var mu sync.Mutex
	var jobs []*Job

	resolve := func(ctx context.Context, dossierID string) (*sql.DB, error) {
		return db, nil
	}
	list := func(ctx context.Context) ([]string, error) {
		return []string{"user-1_space-1"}, nil
	}
	sink := func(ctx context.Context, job *Job) error {
		mu.Lock()
		defer mu.Unlock()
		jobs = append(jobs, job)
		return nil
	}

	sched := New(resolve, list, sink, Config{MaxFailCount: 5}, nil)
	sched.enqueueDueSources(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(jobs) != 2 {
		t.Fatalf("jobs: got %d, want 2", len(jobs))
	}

	ids := map[string]bool{}
	for _, j := range jobs {
		ids[j.SourceID] = true
	}
	if !ids["src-due"] {
		t.Error("src-due should be enqueued")
	}
	if !ids["src-new"] {
		t.Error("src-new should be enqueued")
	}
}

func TestSkipHighFailCount(t *testing.T) {
	// WHAT: Sources with fail_count >= MaxFailCount are skipped.
	// WHY: Prevents hammering broken sources.
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	s := store.NewStore(db)
	s.InsertSource(ctx, &store.Source{ID: "src-fail", Name: "Fail", URL: "https://fail.com", Enabled: true, FailCount: 10})

	var jobs []*Job
	resolve := func(ctx context.Context, dossierID string) (*sql.DB, error) { return db, nil }
	list := func(ctx context.Context) ([]string, error) { return []string{"u_s"}, nil }
	sink := func(ctx context.Context, job *Job) error { jobs = append(jobs, job); return nil }

	sched := New(resolve, list, sink, Config{MaxFailCount: 5}, nil)
	sched.enqueueDueSources(ctx)

	if len(jobs) != 0 {
		t.Errorf("jobs: got %d, want 0 (high fail count should be skipped)", len(jobs))
	}
}
