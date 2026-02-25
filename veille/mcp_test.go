package veille

import (
	"context"
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
	if err := svc.AddSource(ctx, "d1", src); err != nil {
		t.Fatalf("add source: %v", err)
	}
	if src.ID == "" {
		t.Error("ID should be auto-generated")
	}

	sources, err := svc.ListSources(ctx, "d1")
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
	st.InsertExtraction(ctx, &store.Extraction{ID: "ext-1", SourceID: "src-1", ContentHash: "h", ExtractedText: "distributed systems design patterns", URL: "https://s.com", ExtractedAt: now})

	results, err := svc.Search(ctx, "d1", "distributed systems", 10)
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

	svc.AddSource(ctx, "d1", &Source{Name: "S1", URL: "https://s1.com", Enabled: true})
	svc.AddSource(ctx, "d1", &Source{Name: "S2", URL: "https://s2.com", Enabled: true})

	stats, err := svc.Stats(ctx, "d1")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Sources != 2 {
		t.Errorf("sources: got %d, want 2", stats.Sources)
	}
}

func TestService_FetchNow(t *testing.T) {
	// WHAT: FetchNow triggers the pipeline for a source.
	// WHY: Manual fetch is an MCP tool.
	// Note: This test doesn't run against a real HTTP server;
	// it verifies the error path when the source URL is unreachable.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	svc.AddSource(ctx, "d1", &Source{
		Name:    "Unreachable",
		URL:     "http://203.0.113.1:1", // TEST-NET-3 (RFC 5737), unreachable but non-private
		Enabled: true,
	})
	sources, _ := svc.ListSources(ctx, "d1")

	err := svc.FetchNow(ctx, "d1", sources[0].ID)
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}

	// Source should have fail_count incremented.
	src, _ := svc.ListSources(ctx, "d1")
	if src[0].FailCount != 1 {
		t.Errorf("fail_count: got %d, want 1", src[0].FailCount)
	}
}
