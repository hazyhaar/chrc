package pipeline

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hazyhaar/chrc/chunk"
	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/store"

	_ "modernc.org/sqlite"
)

func setupTest(t *testing.T) (*store.Store, func()) {
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
	return store.NewStore(db), func() { db.Close() }
}

func TestHandleJob_Success(t *testing.T) {
	// WHAT: Full pipeline success: fetch → extract → chunk → store.
	// WHY: This is the core pipeline path.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	htmlContent := `<!DOCTYPE html><html><head><title>Test</title></head>
	<body><main><article>
	<h1>Article Title</h1>
	<p>This is the main content of the article. It contains important information
	that should be extracted by the pipeline engine. The text is long enough to
	pass the minimum length threshold for extraction purposes.</p>
	</article></main></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(htmlContent))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{ID: "src-1", Name: "Test", URL: srv.URL, Enabled: true})

	f := fetch.New(fetch.Config{})
	p := New(f, chunk.Options{MaxTokens: 512}, nil)

	err := p.HandleJob(ctx, s, &Job{SourceID: "src-1", URL: srv.URL})
	if err != nil {
		t.Fatalf("handle job: %v", err)
	}

	// Verify source was updated.
	src, _ := s.GetSource(ctx, "src-1")
	if src.LastStatus != "ok" {
		t.Errorf("status: got %q, want ok", src.LastStatus)
	}
	if src.LastHash == "" {
		t.Error("hash should be set")
	}

	// Verify extraction was created.
	exts, _ := s.ListExtractions(ctx, "src-1", 10)
	if len(exts) == 0 {
		t.Fatal("no extractions created")
	}

	// Verify chunks were created.
	chunks, _ := s.ListChunks(ctx, 100, 0)
	if len(chunks) == 0 {
		t.Fatal("no chunks created")
	}

	// Verify fetch log.
	history, _ := s.FetchHistory(ctx, "src-1", 10)
	if len(history) == 0 {
		t.Fatal("no fetch log entry")
	}
	if history[0].Status != "ok" {
		t.Errorf("log status: got %q", history[0].Status)
	}
}

func TestHandleJob_Unchanged(t *testing.T) {
	// WHAT: Same content hash means no re-extraction.
	// WHY: Deduplication prevents redundant work.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	body := `<!DOCTYPE html><html><head><title>Test</title></head>
	<body><main><p>Content that stays the same across fetches and is long enough.</p></main></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{ID: "src-u", Name: "U", URL: srv.URL, Enabled: true})

	f := fetch.New(fetch.Config{})
	p := New(f, chunk.Options{MaxTokens: 512}, nil)

	// First fetch — creates extraction.
	p.HandleJob(ctx, s, &Job{SourceID: "src-u", URL: srv.URL})

	// Second fetch — should detect unchanged.
	p.HandleJob(ctx, s, &Job{SourceID: "src-u", URL: srv.URL})

	// Should have only 1 extraction (not 2).
	exts, _ := s.ListExtractions(ctx, "src-u", 10)
	if len(exts) != 1 {
		t.Errorf("extractions: got %d, want 1 (dedup failed)", len(exts))
	}
}

func TestHandleJob_FetchError(t *testing.T) {
	// WHAT: HTTP errors are recorded and fail_count incremented.
	// WHY: Error handling feeds into scheduler backoff.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{ID: "src-e", Name: "E", URL: srv.URL, Enabled: true})

	f := fetch.New(fetch.Config{})
	p := New(f, chunk.Options{MaxTokens: 512}, nil)

	err := p.HandleJob(ctx, s, &Job{SourceID: "src-e", URL: srv.URL})
	if err == nil {
		t.Fatal("expected error")
	}

	src, _ := s.GetSource(ctx, "src-e")
	if src.FailCount != 1 {
		t.Errorf("fail_count: got %d, want 1", src.FailCount)
	}
	if src.LastStatus != "error" {
		t.Errorf("status: got %q", src.LastStatus)
	}
}

func TestHandleJob_DisabledSource(t *testing.T) {
	// WHAT: Disabled sources are skipped silently.
	// WHY: User can disable without deleting.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	s.InsertSource(ctx, &store.Source{ID: "src-d", Name: "D", URL: "https://example.com", Enabled: false})

	f := fetch.New(fetch.Config{})
	p := New(f, chunk.Options{MaxTokens: 512}, nil)

	err := p.HandleJob(ctx, s, &Job{SourceID: "src-d", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("disabled source should not error: %v", err)
	}
}

func TestHandleJob_304NotModified(t *testing.T) {
	// WHAT: 304 Not Modified is handled as unchanged.
	// WHY: Conditional GET optimization.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return 304 (simulates ETag match).
		w.WriteHeader(304)
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{ID: "src-304", Name: "304", URL: srv.URL, Enabled: true})

	f := fetch.New(fetch.Config{})
	p := New(f, chunk.Options{MaxTokens: 512}, nil)

	err := p.HandleJob(ctx, s, &Job{SourceID: "src-304", URL: srv.URL})
	if err != nil {
		t.Fatalf("304 should not error: %v", err)
	}

	src, _ := s.GetSource(ctx, "src-304")
	if src.LastStatus != "unchanged" {
		t.Errorf("status: got %q, want unchanged", src.LastStatus)
	}
}
