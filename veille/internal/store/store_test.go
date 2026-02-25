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
	for _, table := range []string{"sources", "extractions", "fetch_log"} {
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
	// WHAT: Delete a source cascades to extractions.
	// WHY: Cascade must work to avoid orphaned data.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	s.InsertSource(ctx, &Source{ID: "src-del", Name: "Delete Me", URL: "https://del.com", Enabled: true})
	s.InsertExtraction(ctx, &Extraction{ID: "ext-1", SourceID: "src-del", ContentHash: "abc", ExtractedText: "hello", URL: "https://del.com", ExtractedAt: now})

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

func TestSearchFTS5(t *testing.T) {
	// WHAT: Search via FTS5 on extractions table.
	// WHY: Search is the primary consumer-facing feature.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	s.InsertSource(ctx, &Source{ID: "src-s", Name: "S", URL: "https://s.com", Enabled: true})
	s.InsertExtraction(ctx, &Extraction{ID: "ext-1", SourceID: "src-s", ContentHash: "h1", Title: "Machine Learning Algorithms", ExtractedText: "machine learning algorithms for classification", URL: "https://s.com/ml", ExtractedAt: now})
	s.InsertExtraction(ctx, &Extraction{ID: "ext-2", SourceID: "src-s", ContentHash: "h2", Title: "NLP with Transformers", ExtractedText: "natural language processing with transformers", URL: "https://s.com/nlp", ExtractedAt: now + 1})
	s.InsertExtraction(ctx, &Extraction{ID: "ext-3", SourceID: "src-s", ContentHash: "h3", Title: "Computer Vision", ExtractedText: "computer vision and image recognition tasks", URL: "https://s.com/cv", ExtractedAt: now + 2})

	// Search for "machine learning".
	results, err := s.Search(ctx, "machine learning", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("search should return results")
	}
	if results[0].ExtractionID != "ext-1" {
		t.Errorf("first result ExtractionID: got %s, want ext-1", results[0].ExtractionID)
	}
	if results[0].Title != "Machine Learning Algorithms" {
		t.Errorf("first result Title: got %q", results[0].Title)
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

func TestExtractionExists_Found(t *testing.T) {
	// WHAT: ExtractionExists returns true when a matching extraction exists.
	// WHY: RSS/API dedup depends on this to skip already-seen content.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	s.InsertSource(ctx, &Source{ID: "src-dup", Name: "Dup", URL: "https://dup.com", Enabled: true})
	s.InsertExtraction(ctx, &Extraction{
		ID: "ext-dup", SourceID: "src-dup", ContentHash: "hash-abc",
		ExtractedText: "text", URL: "https://dup.com", ExtractedAt: now,
	})

	exists, err := s.ExtractionExists(ctx, "src-dup", "hash-abc")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Error("should exist")
	}
}

func TestExtractionExists_NotFound(t *testing.T) {
	// WHAT: ExtractionExists returns false for non-matching hash.
	// WHY: New content must not be skipped.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	s.InsertSource(ctx, &Source{ID: "src-new", Name: "New", URL: "https://new.com", Enabled: true})
	s.InsertExtraction(ctx, &Extraction{
		ID: "ext-new", SourceID: "src-new", ContentHash: "hash-xyz",
		ExtractedText: "text", URL: "https://new.com", ExtractedAt: now,
	})

	exists, err := s.ExtractionExists(ctx, "src-new", "hash-different")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if exists {
		t.Error("should not exist for different hash")
	}

	// Also test non-existent source.
	exists2, err := s.ExtractionExists(ctx, "src-nonexistent", "hash-xyz")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if exists2 {
		t.Error("should not exist for non-existent source")
	}
}

func TestInsertSearchEngine(t *testing.T) {
	// WHAT: Insert and retrieve a search engine.
	// WHY: Search engine CRUD is used by the question runner.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	e := &SearchEngine{
		ID:          "brave",
		Name:        "Brave Search",
		Strategy:    "api",
		URLTemplate: "https://api.search.brave.com/res/v1/web/search?q={query}",
		Enabled:     true,
	}
	if err := s.InsertSearchEngine(ctx, e); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := s.GetSearchEngine(ctx, "brave")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("engine not found")
	}
	if got.Name != "Brave Search" {
		t.Errorf("name: got %q", got.Name)
	}
	if got.Strategy != "api" {
		t.Errorf("strategy: got %q", got.Strategy)
	}
	if !got.Enabled {
		t.Error("should be enabled")
	}
}

func TestListSearchEngines(t *testing.T) {
	// WHAT: List returns all search engines ordered by name.
	// WHY: Question runner needs to look up engines.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	s.InsertSearchEngine(ctx, &SearchEngine{ID: "brave", Name: "Brave", Strategy: "api", URLTemplate: "https://brave.com?q={query}", Enabled: true})
	s.InsertSearchEngine(ctx, &SearchEngine{ID: "ddg", Name: "DuckDuckGo", Strategy: "generic", URLTemplate: "https://duckduckgo.com/?q={query}", Enabled: false})

	engines, err := s.ListSearchEngines(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(engines) != 2 {
		t.Fatalf("count: got %d, want 2", len(engines))
	}
	// Ordered by name.
	if engines[0].Name != "Brave" {
		t.Errorf("first: got %q, want Brave", engines[0].Name)
	}
}

func TestDeleteSearchEngine(t *testing.T) {
	// WHAT: Delete removes a search engine.
	// WHY: Cleanup of unused engines.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	s.InsertSearchEngine(ctx, &SearchEngine{ID: "del", Name: "Delete Me", Strategy: "api", URLTemplate: "https://del.com?q={query}", Enabled: true})

	if err := s.DeleteSearchEngine(ctx, "del"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	got, err := s.GetSearchEngine(ctx, "del")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Error("engine should be deleted")
	}
}

func TestInsertAndGetQuestion(t *testing.T) {
	// WHAT: Insert and retrieve a tracked question.
	// WHY: Question CRUD is the foundation of query-centric veille.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	q := &TrackedQuestion{
		ID:          "q-001",
		Text:        "état de l'art LLM inference 2026",
		Keywords:    "LLM inference benchmark 2026",
		Channels:    `["brave"]`,
		ScheduleMs:  86400000,
		MaxResults:  20,
		FollowLinks: true,
		Enabled:     true,
	}
	if err := s.InsertQuestion(ctx, q); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := s.GetQuestion(ctx, "q-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("question not found")
	}
	if got.Text != "état de l'art LLM inference 2026" {
		t.Errorf("text: got %q", got.Text)
	}
	if got.Keywords != "LLM inference benchmark 2026" {
		t.Errorf("keywords: got %q", got.Keywords)
	}
	if !got.FollowLinks {
		t.Error("follow_links should be true")
	}
	if !got.Enabled {
		t.Error("should be enabled")
	}
}

func TestListQuestions(t *testing.T) {
	// WHAT: List returns all questions ordered by creation time desc.
	// WHY: MCP tool veille_list_questions uses this.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	s.InsertQuestion(ctx, &TrackedQuestion{ID: "q-a", Text: "Alpha", Enabled: true, CreatedAt: time.Now().UnixMilli()})
	s.InsertQuestion(ctx, &TrackedQuestion{ID: "q-b", Text: "Beta", Enabled: true, CreatedAt: time.Now().UnixMilli() + 1})

	questions, err := s.ListQuestions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(questions) != 2 {
		t.Fatalf("count: got %d, want 2", len(questions))
	}
	// Newest first.
	if questions[0].ID != "q-b" {
		t.Errorf("first should be q-b, got %s", questions[0].ID)
	}
}

func TestDueQuestions(t *testing.T) {
	// WHAT: DueQuestions returns questions whose next run time has passed.
	// WHY: Scheduler uses this to know which questions need re-running.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	past := now - 172800000 // 2 days ago

	// Due: ran 2 days ago, schedule 24h.
	s.InsertQuestion(ctx, &TrackedQuestion{ID: "q-due", Text: "Due", Enabled: true, ScheduleMs: 86400000, LastRunAt: &past})
	// Not due: ran just now, schedule 24h.
	s.InsertQuestion(ctx, &TrackedQuestion{ID: "q-fresh", Text: "Fresh", Enabled: true, ScheduleMs: 86400000, LastRunAt: &now})
	// Due: never run.
	s.InsertQuestion(ctx, &TrackedQuestion{ID: "q-new", Text: "New", Enabled: true, ScheduleMs: 86400000})
	// Not due: disabled.
	s.InsertQuestion(ctx, &TrackedQuestion{ID: "q-off", Text: "Off", Enabled: false, ScheduleMs: 86400000})

	due, err := s.DueQuestions(ctx)
	if err != nil {
		t.Fatalf("due: %v", err)
	}

	ids := make(map[string]bool)
	for _, q := range due {
		ids[q.ID] = true
	}
	if !ids["q-due"] {
		t.Error("'q-due' should be returned")
	}
	if !ids["q-new"] {
		t.Error("'q-new' (never run) should be returned")
	}
	if ids["q-fresh"] {
		t.Error("'q-fresh' should NOT be returned")
	}
	if ids["q-off"] {
		t.Error("'q-off' (disabled) should NOT be returned")
	}
}

func TestRecordQuestionRun(t *testing.T) {
	// WHAT: RecordQuestionRun updates counters and last_run_at.
	// WHY: Run tracking enables schedule calculation and stats.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	s.InsertQuestion(ctx, &TrackedQuestion{ID: "q-run", Text: "Run me", Enabled: true})

	if err := s.RecordQuestionRun(ctx, "q-run", 5); err != nil {
		t.Fatalf("record run: %v", err)
	}

	got, _ := s.GetQuestion(ctx, "q-run")
	if got.LastRunAt == nil {
		t.Fatal("last_run_at should be set")
	}
	if got.LastResultCount != 5 {
		t.Errorf("last_result_count: got %d, want 5", got.LastResultCount)
	}
	if got.TotalResults != 5 {
		t.Errorf("total_results: got %d, want 5", got.TotalResults)
	}

	// Second run — total should accumulate.
	s.RecordQuestionRun(ctx, "q-run", 3)
	got2, _ := s.GetQuestion(ctx, "q-run")
	if got2.TotalResults != 8 {
		t.Errorf("total_results after 2nd run: got %d, want 8", got2.TotalResults)
	}
}

func TestDeleteQuestion(t *testing.T) {
	// WHAT: Delete removes a question.
	// WHY: User must be able to remove questions.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	s.InsertQuestion(ctx, &TrackedQuestion{ID: "q-del", Text: "Delete me", Enabled: true})

	if err := s.DeleteQuestion(ctx, "q-del"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	got, _ := s.GetQuestion(ctx, "q-del")
	if got != nil {
		t.Error("question should be deleted")
	}
}

func TestGetSourceByURL_Found(t *testing.T) {
	// WHAT: GetSourceByURL returns a source matching the given URL.
	// WHY: Dedup logic needs to check if a URL already exists before inserting.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	s.InsertSource(ctx, &Source{ID: "src-url-1", Name: "Example", URL: "https://example.com/feed", Enabled: true})

	got, err := s.GetSourceByURL(ctx, "https://example.com/feed")
	if err != nil {
		t.Fatalf("get by url: %v", err)
	}
	if got == nil {
		t.Fatal("expected source, got nil")
	}
	if got.ID != "src-url-1" {
		t.Errorf("id: got %q, want src-url-1", got.ID)
	}
}

func TestGetSourceByURL_NotFound(t *testing.T) {
	// WHAT: GetSourceByURL returns nil for non-existent URL.
	// WHY: New URLs must be allowed to be inserted.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	got, err := s.GetSourceByURL(ctx, "https://nonexistent.com")
	if err != nil {
		t.Fatalf("get by url: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestCountSources(t *testing.T) {
	// WHAT: CountSources returns the total number of sources.
	// WHY: Quota enforcement needs accurate source counts.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	count, err := s.CountSources(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("initial count: got %d, want 0", count)
	}

	s.InsertSource(ctx, &Source{ID: "src-c1", Name: "C1", URL: "https://c1.com", Enabled: true})
	s.InsertSource(ctx, &Source{ID: "src-c2", Name: "C2", URL: "https://c2.com", Enabled: true})

	count, err = s.CountSources(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("count: got %d, want 2", count)
	}
}

func TestInsertSource_DuplicateURL_UniqueConstraint(t *testing.T) {
	// WHAT: Inserting two sources with the same URL fails at the DB level.
	// WHY: Safety net — even if service-level dedup is bypassed, the DB must reject dupes.
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	s.InsertSource(ctx, &Source{ID: "src-dup-1", Name: "First", URL: "https://dup.com", Enabled: true})

	err := s.InsertSource(ctx, &Source{ID: "src-dup-2", Name: "Second", URL: "https://dup.com", Enabled: true})
	if err == nil {
		t.Fatal("expected UNIQUE constraint error, got nil")
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
	if stats.FetchLogs != 1 {
		t.Errorf("fetch_logs: got %d", stats.FetchLogs)
	}
}
