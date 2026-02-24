package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

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
	if err := ApplySchema(db); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestApplySchema(t *testing.T) {
	// WHAT: Verify schema creates all tables without error.
	// WHY: Schema is the foundation — if it fails, nothing works.
	db := openTestDB(t)
	// Verify tables exist.
	for _, table := range []string{"sources", "extractions", "chunks", "fetch_log"} {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}
}

func TestInsertAndGetSource(t *testing.T) {
	// WHAT: Insert a source and retrieve it by ID.
	// WHY: Basic CRUD must work for the pipeline to function.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	src := &Source{
		ID:      "src-001",
		Name:    "Example",
		URL:     "https://example.com",
		Enabled: true,
	}
	if err := s.InsertSource(ctx, src); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	got, err := s.GetSource(ctx, "src-001")
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if got == nil {
		t.Fatal("source not found")
	}
	if got.Name != "Example" {
		t.Errorf("name: got %q, want %q", got.Name, "Example")
	}
	if got.URL != "https://example.com" {
		t.Errorf("url: got %q", got.URL)
	}
	if !got.Enabled {
		t.Error("enabled should be true")
	}
	if got.SourceType != "web" {
		t.Errorf("source_type: got %q, want %q", got.SourceType, "web")
	}
}

func TestListSources(t *testing.T) {
	// WHAT: List sources returns all inserted sources.
	// WHY: Source listing is a core MCP tool.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	for i, name := range []string{"Alpha", "Beta", "Gamma"} {
		s.InsertSource(ctx, &Source{
			ID:      "src-" + name,
			Name:    name,
			URL:     "https://" + name + ".com",
			Enabled: true,
			CreatedAt: time.Now().UnixMilli() + int64(i),
		})
	}

	sources, err := s.ListSources(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sources) != 3 {
		t.Fatalf("count: got %d, want 3", len(sources))
	}
}

func TestUpdateSource(t *testing.T) {
	// WHAT: Update mutable source fields.
	// WHY: Source editing is an MCP tool.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	s.InsertSource(ctx, &Source{ID: "src-1", Name: "Old", URL: "https://old.com", Enabled: true})

	src, _ := s.GetSource(ctx, "src-1")
	src.Name = "New"
	src.URL = "https://new.com"
	if err := s.UpdateSource(ctx, src); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := s.GetSource(ctx, "src-1")
	if got.Name != "New" {
		t.Errorf("name: got %q", got.Name)
	}
	if got.URL != "https://new.com" {
		t.Errorf("url: got %q", got.URL)
	}
}

func TestDeleteSource(t *testing.T) {
	// WHAT: Delete a source cascades to extractions and chunks.
	// WHY: Cascade must work to avoid orphaned data.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	s.InsertSource(ctx, &Source{ID: "src-del", Name: "Delete Me", URL: "https://del.com", Enabled: true})
	s.InsertExtraction(ctx, &Extraction{ID: "ext-1", SourceID: "src-del", ContentHash: "abc", ExtractedText: "hello", URL: "https://del.com", ExtractedAt: now})
	s.InsertChunks(ctx, []*Chunk{{ID: "ch-1", ExtractionID: "ext-1", SourceID: "src-del", Text: "hello", TokenCount: 1, CreatedAt: now}})

	if err := s.DeleteSource(ctx, "src-del"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	got, _ := s.GetSource(ctx, "src-del")
	if got != nil {
		t.Error("source should be deleted")
	}

	// Extraction should be cascaded.
	ext, _ := s.GetExtraction(ctx, "ext-1")
	if ext != nil {
		t.Error("extraction should be cascade-deleted")
	}
}

func TestDueSources(t *testing.T) {
	// WHAT: DueSources returns sources whose next fetch time has passed.
	// WHY: Scheduler relies on this to know what to fetch.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	past := now - 7200000 // 2 hours ago

	// Due: fetched 2h ago, interval 1h.
	s.InsertSource(ctx, &Source{ID: "due", Name: "Due", URL: "https://due.com", Enabled: true, FetchInterval: 3600000, LastFetchedAt: &past})
	// Not due: fetched just now, interval 1h.
	s.InsertSource(ctx, &Source{ID: "fresh", Name: "Fresh", URL: "https://fresh.com", Enabled: true, FetchInterval: 3600000, LastFetchedAt: &now})
	// Due: never fetched.
	s.InsertSource(ctx, &Source{ID: "new", Name: "New", URL: "https://new.com", Enabled: true})
	// Not due: disabled.
	s.InsertSource(ctx, &Source{ID: "disabled", Name: "Off", URL: "https://off.com", Enabled: false})
	// Not due: too many failures.
	s.InsertSource(ctx, &Source{ID: "failing", Name: "Fail", URL: "https://fail.com", Enabled: true, FailCount: 10})

	due, err := s.DueSources(ctx, 5)
	if err != nil {
		t.Fatalf("due sources: %v", err)
	}

	ids := make(map[string]bool)
	for _, d := range due {
		ids[d.ID] = true
	}
	if !ids["due"] {
		t.Error("'due' should be returned")
	}
	if !ids["new"] {
		t.Error("'new' (never fetched) should be returned")
	}
	if ids["fresh"] {
		t.Error("'fresh' should NOT be returned")
	}
	if ids["disabled"] {
		t.Error("'disabled' should NOT be returned")
	}
	if ids["failing"] {
		t.Error("'failing' (fail_count >= 5) should NOT be returned")
	}
}

func TestRecordFetchSuccess(t *testing.T) {
	// WHAT: RecordFetchSuccess updates source state.
	// WHY: Pipeline must record success to prevent re-fetching.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	s.InsertSource(ctx, &Source{ID: "src-ok", Name: "OK", URL: "https://ok.com", Enabled: true})
	if err := s.RecordFetchSuccess(ctx, "src-ok", "hash123"); err != nil {
		t.Fatalf("record success: %v", err)
	}

	got, _ := s.GetSource(ctx, "src-ok")
	if got.LastHash != "hash123" {
		t.Errorf("hash: got %q", got.LastHash)
	}
	if got.LastStatus != "ok" {
		t.Errorf("status: got %q", got.LastStatus)
	}
	if got.FailCount != 0 {
		t.Errorf("fail_count: got %d", got.FailCount)
	}
}

func TestRecordFetchError(t *testing.T) {
	// WHAT: RecordFetchError increments fail_count.
	// WHY: Scheduler skips sources with too many failures.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	s.InsertSource(ctx, &Source{ID: "src-err", Name: "Err", URL: "https://err.com", Enabled: true})
	s.RecordFetchError(ctx, "src-err", "timeout")
	s.RecordFetchError(ctx, "src-err", "timeout again")

	got, _ := s.GetSource(ctx, "src-err")
	if got.FailCount != 2 {
		t.Errorf("fail_count: got %d, want 2", got.FailCount)
	}
	if got.LastStatus != "error" {
		t.Errorf("status: got %q", got.LastStatus)
	}
}

func TestInsertAndListExtractions(t *testing.T) {
	// WHAT: Insert and list extractions for a source.
	// WHY: Extraction CRUD is used by pipeline and MCP.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	s.InsertSource(ctx, &Source{ID: "src-ex", Name: "Ex", URL: "https://ex.com", Enabled: true})
	s.InsertExtraction(ctx, &Extraction{ID: "ext-1", SourceID: "src-ex", ContentHash: "a", ExtractedText: "text1", URL: "https://ex.com/1", ExtractedAt: now})
	s.InsertExtraction(ctx, &Extraction{ID: "ext-2", SourceID: "src-ex", ContentHash: "b", ExtractedText: "text2", URL: "https://ex.com/2", ExtractedAt: now + 1})

	exts, err := s.ListExtractions(ctx, "src-ex", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(exts) != 2 {
		t.Fatalf("count: got %d, want 2", len(exts))
	}
	// Newest first.
	if exts[0].ID != "ext-2" {
		t.Errorf("first should be ext-2, got %s", exts[0].ID)
	}
}

func TestInsertChunksAndSearch(t *testing.T) {
	// WHAT: Insert chunks and search via FTS5.
	// WHY: Search is the primary consumer-facing feature.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	s.InsertSource(ctx, &Source{ID: "src-s", Name: "S", URL: "https://s.com", Enabled: true})
	s.InsertExtraction(ctx, &Extraction{ID: "ext-s", SourceID: "src-s", ContentHash: "h", ExtractedText: "full text", URL: "https://s.com", ExtractedAt: now})

	chunks := []*Chunk{
		{ID: "ch-1", ExtractionID: "ext-s", SourceID: "src-s", ChunkIndex: 0, Text: "machine learning algorithms for classification", TokenCount: 6, CreatedAt: now},
		{ID: "ch-2", ExtractionID: "ext-s", SourceID: "src-s", ChunkIndex: 1, Text: "natural language processing with transformers", TokenCount: 6, CreatedAt: now},
		{ID: "ch-3", ExtractionID: "ext-s", SourceID: "src-s", ChunkIndex: 2, Text: "computer vision and image recognition tasks", TokenCount: 7, CreatedAt: now},
	}
	if err := s.InsertChunks(ctx, chunks); err != nil {
		t.Fatalf("insert chunks: %v", err)
	}

	// Search for "machine learning".
	results, err := s.Search(ctx, "machine learning", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("search should return results")
	}
	if results[0].ChunkID != "ch-1" {
		t.Errorf("first result: got %s, want ch-1", results[0].ChunkID)
	}
}

func TestListChunks(t *testing.T) {
	// WHAT: ListChunks returns paginated chunks.
	// WHY: MCP tool veille_list_chunks uses this.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	s.InsertSource(ctx, &Source{ID: "src-lc", Name: "LC", URL: "https://lc.com", Enabled: true})
	s.InsertExtraction(ctx, &Extraction{ID: "ext-lc", SourceID: "src-lc", ContentHash: "h", ExtractedText: "text", URL: "https://lc.com", ExtractedAt: now})
	s.InsertChunks(ctx, []*Chunk{
		{ID: "ch-a", ExtractionID: "ext-lc", SourceID: "src-lc", ChunkIndex: 0, Text: "chunk a", TokenCount: 2, CreatedAt: now},
		{ID: "ch-b", ExtractionID: "ext-lc", SourceID: "src-lc", ChunkIndex: 1, Text: "chunk b", TokenCount: 2, CreatedAt: now + 1},
	})

	chunks, err := s.ListChunks(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("count: got %d, want 2", len(chunks))
	}
}

func TestFetchLog(t *testing.T) {
	// WHAT: Insert and retrieve fetch log entries.
	// WHY: Observability requires fetch history.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	s.InsertSource(ctx, &Source{ID: "src-fl", Name: "FL", URL: "https://fl.com", Enabled: true})
	s.InsertFetchLog(ctx, &FetchLogEntry{ID: "fl-1", SourceID: "src-fl", Status: "ok", StatusCode: 200, DurationMs: 150, FetchedAt: now})
	s.InsertFetchLog(ctx, &FetchLogEntry{ID: "fl-2", SourceID: "src-fl", Status: "error", StatusCode: 500, ErrorMessage: "server error", DurationMs: 50, FetchedAt: now + 1})

	history, err := s.FetchHistory(ctx, "src-fl", 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("count: got %d, want 2", len(history))
	}
	// Newest first.
	if history[0].Status != "error" {
		t.Errorf("first should be error, got %s", history[0].Status)
	}
}

func TestStats(t *testing.T) {
	// WHAT: Stats returns correct aggregate counts.
	// WHY: MCP tool veille_stats uses this.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	s.InsertSource(ctx, &Source{ID: "src-st", Name: "St", URL: "https://st.com", Enabled: true})
	s.InsertExtraction(ctx, &Extraction{ID: "ext-st", SourceID: "src-st", ContentHash: "h", ExtractedText: "t", URL: "https://st.com", ExtractedAt: now})
	s.InsertChunks(ctx, []*Chunk{
		{ID: "ch-st1", ExtractionID: "ext-st", SourceID: "src-st", ChunkIndex: 0, Text: "a", TokenCount: 1, CreatedAt: now},
		{ID: "ch-st2", ExtractionID: "ext-st", SourceID: "src-st", ChunkIndex: 1, Text: "b", TokenCount: 1, CreatedAt: now},
	})
	s.InsertFetchLog(ctx, &FetchLogEntry{ID: "fl-st", SourceID: "src-st", Status: "ok", FetchedAt: now})

	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Sources != 1 {
		t.Errorf("sources: got %d", stats.Sources)
	}
	if stats.Extractions != 1 {
		t.Errorf("extractions: got %d", stats.Extractions)
	}
	if stats.Chunks != 2 {
		t.Errorf("chunks: got %d", stats.Chunks)
	}
	if stats.FetchLogs != 1 {
		t.Errorf("fetch_logs: got %d", stats.FetchLogs)
	}
}
