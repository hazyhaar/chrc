package veille

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/store"

	_ "modernc.org/sqlite"
)

func TestService_AddSource(t *testing.T) {
	// WHAT: Add a source via service layer.
	// WHY: Service layer is the API surface.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	src := &Source{Name: "Test Source", URL: "https://test.com", Enabled: true}
	if err := svc.AddSource(ctx, "u1", "s1", src); err != nil {
		t.Fatalf("add source: %v", err)
	}
	if src.ID == "" {
		t.Error("ID should be auto-generated")
	}

	sources, err := svc.ListSources(ctx, "u1", "s1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("count: got %d", len(sources))
	}
}

func TestService_Search(t *testing.T) {
	// WHAT: FTS5 search via service layer.
	// WHY: Search is the primary consumer feature.
	svc, db := setupTestService(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	st := store.NewStore(db)
	st.InsertSource(ctx, &store.Source{ID: "src-1", Name: "S", URL: "https://s.com", Enabled: true})
	st.InsertExtraction(ctx, &store.Extraction{ID: "ext-1", SourceID: "src-1", ContentHash: "h", ExtractedText: "text", URL: "https://s.com", ExtractedAt: now})
	st.InsertChunks(ctx, []*store.Chunk{
		{ID: "ch-1", ExtractionID: "ext-1", SourceID: "src-1", ChunkIndex: 0, Text: "distributed systems design patterns", TokenCount: 4, CreatedAt: now},
	})

	results, err := svc.Search(ctx, "u1", "s1", "distributed systems", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("should find results")
	}
}

func TestService_Stats(t *testing.T) {
	// WHAT: Stats via service layer.
	// WHY: Validates shard resolution + store integration.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	svc.AddSource(ctx, "u1", "s1", &Source{Name: "S1", URL: "https://s1.com", Enabled: true})
	svc.AddSource(ctx, "u1", "s1", &Source{Name: "S2", URL: "https://s2.com", Enabled: true})

	stats, err := svc.Stats(ctx, "u1", "s1")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Sources != 2 {
		t.Errorf("sources: got %d, want 2", stats.Sources)
	}
}

func TestService_CreateSpace(t *testing.T) {
	// WHAT: Create a space via service layer.
	// WHY: Space creation initializes the shard schema.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	// Pre-apply schema since the test pool always returns same DB.
	store.ApplySchema(db)
	t.Cleanup(func() { db.Close() })

	pool := &testPool{db: db}
	spaces := &testSpaces{}
	svc, _ := New(pool, spaces, nil, nil)

	space, err := svc.CreateSpace(context.Background(), "user-1", "My Watch")
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	if space.UserID != "user-1" {
		t.Errorf("user_id: got %q", space.UserID)
	}
	if space.Name != "My Watch" {
		t.Errorf("name: got %q", space.Name)
	}
	if space.SpaceID == "" {
		t.Error("space_id should be generated")
	}

	listed, err := svc.ListSpaces(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("list spaces: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("count: got %d, want 1", len(listed))
	}
}

func TestService_FetchNow(t *testing.T) {
	// WHAT: FetchNow triggers the pipeline for a source.
	// WHY: Manual fetch is an MCP tool.
	// Note: This test doesn't run against a real HTTP server;
	// it verifies the error path when the source URL is unreachable.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	svc.AddSource(ctx, "u1", "s1", &Source{
		Name:    "Unreachable",
		URL:     "http://127.0.0.1:1", // unreachable
		Enabled: true,
	})
	sources, _ := svc.ListSources(ctx, "u1", "s1")

	err := svc.FetchNow(ctx, "u1", "s1", sources[0].ID)
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}

	// Source should have fail_count incremented.
	src, _ := svc.ListSources(ctx, "u1", "s1")
	if src[0].FailCount != 1 {
		t.Errorf("fail_count: got %d, want 1", src[0].FailCount)
	}
}
