package question

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/search"
	"github.com/hazyhaar/chrc/veille/internal/store"

	_ "modernc.org/sqlite"
)

var idCounter int

func testID() string {
	idCounter++
	return fmt.Sprintf("id-%03d", idCounter)
}

func openTestDB(t *testing.T) *store.Store {
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
	t.Cleanup(func() { db.Close() })
	return store.NewStore(db)
}

func mockEngine(id string) *search.Engine {
	return &search.Engine{
		ID:       id,
		Name:     "Mock " + id,
		Strategy: "api",
		Enabled:  true,
	}
}

func mockSearcher(results []search.Result) func(context.Context, *search.Engine, string) ([]search.Result, error) {
	return func(_ context.Context, _ *search.Engine, _ string) ([]search.Result, error) {
		return results, nil
	}
}

func TestRun_SingleChannel(t *testing.T) {
	// WHAT: Run a question against one engine, verify extractions created.
	// WHY: Basic question runner path must produce extractions.
	s := openTestDB(t)
	ctx := context.Background()
	idCounter = 0

	// Create the source row for the question (sourceID = q.ID).
	s.InsertSource(ctx, &store.Source{ID: "q-1", Name: "Q: Test", URL: "question://q-1", SourceType: "question", Enabled: true})

	q := &store.TrackedQuestion{
		ID:          "q-1",
		Text:        "golang concurrency",
		Channels:    `["brave"]`,
		ScheduleMs:  86400000,
		MaxResults:  10,
		FollowLinks: false,
		Enabled:     true,
	}
	s.InsertQuestion(ctx, q)

	runner := NewRunner(Config{
		Engines: func(_ context.Context, id string) (*search.Engine, error) {
			return mockEngine(id), nil
		},
		Searcher: mockSearcher([]search.Result{
			{Title: "Go Concurrency Patterns", URL: "https://go.dev/concurrency", Snippet: "Go provides goroutines and channels for concurrent programming."},
			{Title: "Go Routines", URL: "https://go.dev/goroutines", Snippet: "Goroutines are lightweight threads managed by the Go runtime."},
		}),
		NewID:     testID,
	})

	count, err := runner.Run(ctx, s, q, "d1")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if count != 2 {
		t.Errorf("new count: got %d, want 2", count)
	}

	// Verify extractions (sourceID = q.ID).
	exts, _ := s.ListExtractions(ctx, "q-1", 10)
	if len(exts) != 2 {
		t.Fatalf("extractions: got %d, want 2", len(exts))
	}

	// Verify metadata contains question_id.
	var meta map[string]string
	json.Unmarshal([]byte(exts[0].MetadataJSON), &meta)
	if meta["question_id"] != "q-1" {
		t.Errorf("metadata question_id: got %q", meta["question_id"])
	}

	// Verify question run was recorded.
	got, _ := s.GetQuestion(ctx, "q-1")
	if got.LastResultCount != 2 {
		t.Errorf("last_result_count: got %d", got.LastResultCount)
	}
}

func TestRun_Dedup(t *testing.T) {
	// WHAT: Second run with same URLs produces 0 new results.
	// WHY: Dedup prevents duplicate extractions across runs.
	s := openTestDB(t)
	ctx := context.Background()
	idCounter = 100

	s.InsertSource(ctx, &store.Source{ID: "q-dup", Name: "Q: Dedup", URL: "question://q-dup", SourceType: "question", Enabled: true})

	q := &store.TrackedQuestion{
		ID:       "q-dup",
		Text:     "test dedup",
		Channels: `["brave"]`,
		Enabled:  true,
	}
	s.InsertQuestion(ctx, q)

	results := []search.Result{
		{Title: "Result A", URL: "https://example.com/a", Snippet: "Some content about A topic for testing."},
	}

	runner := NewRunner(Config{
		Engines:  func(_ context.Context, _ string) (*search.Engine, error) { return mockEngine("brave"), nil },
		Searcher: mockSearcher(results),
		NewID:    testID,
	})

	// First run.
	c1, _ := runner.Run(ctx, s, q, "d1")
	if c1 != 1 {
		t.Fatalf("first run: got %d, want 1", c1)
	}

	// Second run — same URL → dedup.
	c2, _ := runner.Run(ctx, s, q, "d1")
	if c2 != 0 {
		t.Errorf("second run: got %d, want 0 (dedup)", c2)
	}
}

func TestRun_FollowLinks(t *testing.T) {
	// WHAT: With FollowLinks=true but no fetcher, falls back to snippet.
	// WHY: Graceful degradation when fetcher is nil.
	s := openTestDB(t)
	ctx := context.Background()
	idCounter = 200

	s.InsertSource(ctx, &store.Source{ID: "q-fl", Name: "Q: Follow", URL: "question://q-fl", SourceType: "question", Enabled: true})

	q := &store.TrackedQuestion{
		ID:          "q-fl",
		Text:        "follow links test",
		Channels:    `["brave"]`,
		FollowLinks: true,
		Enabled:     true,
	}
	s.InsertQuestion(ctx, q)

	runner := NewRunner(Config{
		Engines:  func(_ context.Context, _ string) (*search.Engine, error) { return mockEngine("brave"), nil },
		Searcher: mockSearcher([]search.Result{
			{Title: "Page", URL: "https://example.com/page", Snippet: "This is the snippet content for the page."},
		}),
		Fetcher:   nil, // no fetcher → fallback to snippet
		NewID:     testID,
	})

	count, err := runner.Run(ctx, s, q, "d1")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if count != 1 {
		t.Errorf("count: got %d, want 1", count)
	}

	exts, _ := s.ListExtractions(ctx, "q-fl", 10)
	if len(exts) != 1 {
		t.Fatalf("extractions: got %d", len(exts))
	}
	if !strings.Contains(exts[0].ExtractedText, "snippet content") {
		t.Errorf("expected snippet content, got: %q", exts[0].ExtractedText)
	}
}

func TestRun_SnippetOnly(t *testing.T) {
	// WHAT: With FollowLinks=false, only snippets are stored.
	// WHY: Snippet-only mode is faster and sufficient for monitoring.
	s := openTestDB(t)
	ctx := context.Background()
	idCounter = 300

	s.InsertSource(ctx, &store.Source{ID: "q-sn", Name: "Q: Snippet", URL: "question://q-sn", SourceType: "question", Enabled: true})

	q := &store.TrackedQuestion{
		ID:          "q-sn",
		Text:        "snippet only",
		Channels:    `["brave"]`,
		FollowLinks: false,
		Enabled:     true,
	}
	s.InsertQuestion(ctx, q)

	runner := NewRunner(Config{
		Engines:  func(_ context.Context, _ string) (*search.Engine, error) { return mockEngine("brave"), nil },
		Searcher: mockSearcher([]search.Result{
			{Title: "Snippet Result", URL: "https://example.com/snippet", Snippet: "Only the snippet is stored not the full page."},
		}),
		NewID:     testID,
	})

	count, _ := runner.Run(ctx, s, q, "d1")
	if count != 1 {
		t.Errorf("count: got %d, want 1", count)
	}

	exts, _ := s.ListExtractions(ctx, "q-sn", 10)
	if len(exts) != 1 {
		t.Fatalf("extractions: got %d", len(exts))
	}
	if !strings.Contains(exts[0].ExtractedText, "snippet is stored") {
		t.Errorf("expected snippet, got: %q", exts[0].ExtractedText)
	}
}

func TestRun_MultiChannel(t *testing.T) {
	// WHAT: Question with multiple channels collects results from all.
	// WHY: Multi-engine coverage is a key feature.
	s := openTestDB(t)
	ctx := context.Background()
	idCounter = 400

	s.InsertSource(ctx, &store.Source{ID: "q-mc", Name: "Q: Multi", URL: "question://q-mc", SourceType: "question", Enabled: true})

	q := &store.TrackedQuestion{
		ID:       "q-mc",
		Text:     "multi channel",
		Channels: `["brave", "ddg"]`,
		Enabled:  true,
	}
	s.InsertQuestion(ctx, q)

	callCount := 0
	runner := NewRunner(Config{
		Engines: func(_ context.Context, id string) (*search.Engine, error) {
			return mockEngine(id), nil
		},
		Searcher: func(_ context.Context, engine *search.Engine, _ string) ([]search.Result, error) {
			callCount++
			return []search.Result{
				{Title: "From " + engine.ID, URL: "https://" + engine.ID + ".com/result", Snippet: "Result from " + engine.ID + " engine search."},
			}, nil
		},
		NewID:     testID,
	})

	count, _ := runner.Run(ctx, s, q, "d1")
	if count != 2 {
		t.Errorf("count: got %d, want 2", count)
	}
	if callCount != 2 {
		t.Errorf("engines called: got %d, want 2", callCount)
	}
}

func TestRun_WritesBuffer(t *testing.T) {
	// WHAT: When buffer is configured, .md files are written.
	// WHY: Buffer output feeds the RAG island.
	s := openTestDB(t)
	ctx := context.Background()
	idCounter = 500

	s.InsertSource(ctx, &store.Source{ID: "q-buf", Name: "Q: Buffer", URL: "question://q-buf", SourceType: "question", Enabled: true})

	q := &store.TrackedQuestion{
		ID:       "q-buf",
		Text:     "buffer test",
		Channels: `["brave"]`,
		Enabled:  true,
	}
	s.InsertQuestion(ctx, q)

	bufDir := filepath.Join(t.TempDir(), "pending")
	runner := NewRunner(Config{
		Engines:  func(_ context.Context, _ string) (*search.Engine, error) { return mockEngine("brave"), nil },
		Searcher: mockSearcher([]search.Result{
			{Title: "Buffer Test", URL: "https://example.com/buf", Snippet: "Content for buffer test should be written to pending dir."},
		}),
		Buffer:    buffer.NewWriter(bufDir),
		NewID:     testID,
	})

	runner.Run(ctx, s, q, "d1")

	entries, err := os.ReadDir(bufDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no .md files in buffer dir")
	}

	data, _ := os.ReadFile(filepath.Join(bufDir, entries[0].Name()))
	content := string(data)
	if !strings.Contains(content, "source_type: question") {
		t.Error("frontmatter missing source_type: question")
	}
	if !strings.Contains(content, "source_id: q-buf") {
		t.Error("frontmatter missing source_id")
	}
}
