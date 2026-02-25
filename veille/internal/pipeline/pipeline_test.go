package pipeline

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hazyhaar/chrc/veille/internal/buffer"
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
	// WHAT: Full pipeline success: fetch → extract → store.
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
	p := New(f, nil)

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
	p := New(f, nil)

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
	p := New(f, nil)

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
	p := New(f, nil)

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
	p := New(f, nil)

	err := p.HandleJob(ctx, s, &Job{SourceID: "src-304", URL: srv.URL})
	if err != nil {
		t.Fatalf("304 should not error: %v", err)
	}

	src, _ := s.GetSource(ctx, "src-304")
	if src.LastStatus != "unchanged" {
		t.Errorf("status: got %q, want unchanged", src.LastStatus)
	}
}

func TestPipeline_WritesBufferMD(t *testing.T) {
	// WHAT: When buffer is configured, a .md file is written to pending.
	// WHY: Buffer output feeds the RAG island.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	htmlContent := `<!DOCTYPE html><html><head><title>Buffer Test</title></head>
	<body><main><article>
	<p>Content for buffer testing. This is long enough to be extracted and should
	produce a markdown file in the pending directory for RAG consumption.</p>
	</article></main></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(htmlContent))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{ID: "src-buf", Name: "BufTest", URL: srv.URL, Enabled: true})

	bufDir := filepath.Join(t.TempDir(), "pending")
	f := fetch.New(fetch.Config{})
	p := New(f, nil)
	p.SetBuffer(buffer.NewWriter(bufDir))

	err := p.HandleJob(ctx, s, &Job{DossierID: "user-A_tech", SourceID: "src-buf", URL: srv.URL})
	if err != nil {
		t.Fatalf("handle job: %v", err)
	}

	// Verify .md file was created.
	entries, err := os.ReadDir(bufDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no .md files in buffer pending dir")
	}

	// Read the .md and verify frontmatter.
	data, _ := os.ReadFile(filepath.Join(bufDir, entries[0].Name()))
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		t.Error("file should start with frontmatter ---")
	}
	if !strings.Contains(content, "source_id: src-buf") {
		t.Error("frontmatter missing source_id")
	}
	if !strings.Contains(content, "dossier_id: user-A_tech") {
		t.Error("frontmatter missing dossier_id")
	}
	if !strings.Contains(content, "source_type: web") {
		t.Error("frontmatter missing source_type")
	}
}

func TestPipeline_NoBufferIfNil(t *testing.T) {
	// WHAT: Without buffer configured, no .md files are created.
	// WHY: Buffer is optional — existing behavior must not break.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	htmlContent := `<!DOCTYPE html><html><head><title>No Buffer</title></head>
	<body><main><p>Content without buffer. Long enough to extract properly.</p></main></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(htmlContent))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{ID: "src-nb", Name: "NoBuf", URL: srv.URL, Enabled: true})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)
	// No SetBuffer call — buffer is nil.

	err := p.HandleJob(ctx, s, &Job{SourceID: "src-nb", URL: srv.URL})
	if err != nil {
		t.Fatalf("handle job: %v", err)
	}

	// Verify extraction was still created.
	exts, _ := s.ListExtractions(ctx, "src-nb", 10)
	if len(exts) == 0 {
		t.Fatal("extraction should still be created without buffer")
	}
}

func TestDispatch_Web(t *testing.T) {
	// WHAT: Source with type "web" dispatches to WebHandler.
	// WHY: Explicit web type should use the web handler.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	htmlContent := `<!DOCTYPE html><html><head><title>Web</title></head>
	<body><main><p>Web dispatch test content, needs to be long enough to pass extraction.</p></main></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(htmlContent))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{ID: "src-w", Name: "Web", URL: srv.URL, SourceType: "web", Enabled: true})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	err := p.HandleJob(ctx, s, &Job{SourceID: "src-w", URL: srv.URL})
	if err != nil {
		t.Fatalf("handle job: %v", err)
	}

	exts, _ := s.ListExtractions(ctx, "src-w", 10)
	if len(exts) == 0 {
		t.Fatal("web dispatch should create extractions")
	}
}

func TestDispatch_UnknownFallsBackToWeb(t *testing.T) {
	// WHAT: Unknown source_type falls back to web handler.
	// WHY: Graceful degradation for unregistered types.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	htmlContent := `<!DOCTYPE html><html><head><title>Unknown</title></head>
	<body><main><p>Unknown type test content should still work via web fallback handler.</p></main></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(htmlContent))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{ID: "src-unk", Name: "Unknown", URL: srv.URL, SourceType: "telegram", Enabled: true})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	err := p.HandleJob(ctx, s, &Job{SourceID: "src-unk", URL: srv.URL})
	if err != nil {
		t.Fatalf("handle job: %v", err)
	}

	exts, _ := s.ListExtractions(ctx, "src-unk", 10)
	if len(exts) == 0 {
		t.Fatal("unknown type should fallback to web and create extractions")
	}
}
