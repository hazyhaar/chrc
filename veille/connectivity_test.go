package veille

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/store"

	_ "modernc.org/sqlite"
)

// testPool implements PoolResolver with a single in-memory DB.
type testPool struct {
	db *sql.DB
}

func (tp *testPool) Resolve(_ context.Context, _, _ string) (*sql.DB, error) {
	return tp.db, nil
}

// testSpaces implements SpaceManager for testing.
type testSpaces struct {
	spaces []SpaceInfo
}

func (ts *testSpaces) CreateSpace(_ context.Context, userID, spaceID, name string) error {
	ts.spaces = append(ts.spaces, SpaceInfo{UserID: userID, SpaceID: spaceID, Name: name})
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

func (ts *testSpaces) ListSpaces(_ context.Context, userID string) ([]SpaceInfo, error) {
	var result []SpaceInfo
	for _, s := range ts.spaces {
		if s.UserID == userID {
			result = append(result, s)
		}
	}
	return result, nil
}

func setupTestService(t *testing.T) (*Service, *sql.DB) {
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

	pool := &testPool{db: db}
	spaces := &testSpaces{}
	svc, err := New(pool, spaces, nil, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc, db
}

func callConn(t *testing.T, handler func(context.Context, []byte) ([]byte, error), payload any) []byte {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := handler(context.Background(), data)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return resp
}

func TestConnectivity_AddAndListSources(t *testing.T) {
	// WHAT: Add a source via connectivity, then list.
	// WHY: Validates connectivity handler wiring.
	svc, _ := setupTestService(t)

	resp := callConn(t, svc.handleAddSource, map[string]any{
		"user_id": "u1", "space_id": "s1", "name": "Test", "url": "https://test.com",
	})
	var src Source
	json.Unmarshal(resp, &src)
	if src.ID == "" {
		t.Error("source ID should be set")
	}

	resp = callConn(t, svc.handleListSources, map[string]any{
		"user_id": "u1", "space_id": "s1",
	})
	var sources []*Source
	json.Unmarshal(resp, &sources)
	if len(sources) != 1 {
		t.Fatalf("count: got %d, want 1", len(sources))
	}
	if sources[0].Name != "Test" {
		t.Errorf("name: got %q", sources[0].Name)
	}
}

func TestConnectivity_Search(t *testing.T) {
	// WHAT: Search via connectivity handler.
	// WHY: Search is the primary consumer-facing feature.
	svc, db := setupTestService(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	st := store.NewStore(db)
	st.InsertSource(ctx, &store.Source{ID: "src-1", Name: "S", URL: "https://s.com", Enabled: true})
	st.InsertExtraction(ctx, &store.Extraction{ID: "ext-1", SourceID: "src-1", ContentHash: "h", ExtractedText: "text", URL: "https://s.com", ExtractedAt: now})
	st.InsertChunks(ctx, []*store.Chunk{
		{ID: "ch-1", ExtractionID: "ext-1", SourceID: "src-1", ChunkIndex: 0, Text: "golang concurrency patterns", TokenCount: 3, CreatedAt: now},
	})

	resp := callConn(t, svc.handleSearchConn, map[string]any{
		"user_id": "u1", "space_id": "s1", "query": "golang", "limit": 10,
	})
	var results []*SearchResult
	json.Unmarshal(resp, &results)
	if len(results) == 0 {
		t.Fatal("search should return results")
	}
}

func TestConnectivity_Stats(t *testing.T) {
	// WHAT: Stats via connectivity handler.
	// WHY: Stats is an MCP tool.
	svc, db := setupTestService(t)
	ctx := context.Background()

	st := store.NewStore(db)
	st.InsertSource(ctx, &store.Source{ID: "src-st", Name: "St", URL: "https://st.com", Enabled: true})

	resp := callConn(t, svc.handleStats, map[string]any{
		"user_id": "u1", "space_id": "s1",
	})
	var stats SpaceStats
	json.Unmarshal(resp, &stats)
	if stats.Sources != 1 {
		t.Errorf("sources: got %d, want 1", stats.Sources)
	}
}

func TestConnectivity_FetchHistory(t *testing.T) {
	// WHAT: Fetch history via connectivity handler.
	// WHY: Observability requires fetch history.
	svc, db := setupTestService(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	st := store.NewStore(db)
	st.InsertSource(ctx, &store.Source{ID: "src-fh", Name: "FH", URL: "https://fh.com", Enabled: true})
	st.InsertFetchLog(ctx, &store.FetchLogEntry{ID: "fl-1", SourceID: "src-fh", Status: "ok", StatusCode: 200, FetchedAt: now})

	resp := callConn(t, svc.handleFetchHistory, map[string]any{
		"user_id": "u1", "space_id": "s1", "source_id": "src-fh", "limit": 10,
	})
	var history []*FetchLogEntry
	json.Unmarshal(resp, &history)
	if len(history) != 1 {
		t.Fatalf("count: got %d, want 1", len(history))
	}
}

func TestConnectivity_CreateAndListSpaces(t *testing.T) {
	// WHAT: Create and list spaces via connectivity.
	// WHY: Space management is a core MCP feature.
	svc, _ := setupTestService(t)

	resp := callConn(t, svc.handleCreateSpace, map[string]any{
		"user_id": "u1", "name": "Tech Watch",
	})
	var space SpaceInfo
	json.Unmarshal(resp, &space)
	if space.SpaceID == "" {
		t.Error("space ID should be set")
	}
	if space.Name != "Tech Watch" {
		t.Errorf("name: got %q", space.Name)
	}

	resp = callConn(t, svc.handleListSpaces, map[string]any{
		"user_id": "u1",
	})
	var spaces []SpaceInfo
	json.Unmarshal(resp, &spaces)
	if len(spaces) != 1 {
		t.Fatalf("count: got %d, want 1", len(spaces))
	}
}

func TestConnectivity_DeleteSource(t *testing.T) {
	// WHAT: Delete source via connectivity.
	// WHY: Source deletion must cascade cleanly.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	svc.AddSource(ctx, "u1", "s1", &Source{Name: "Del", URL: "https://del.com", Enabled: true})
	sources, _ := svc.ListSources(ctx, "u1", "s1")
	if len(sources) != 1 {
		t.Fatalf("precondition: got %d sources", len(sources))
	}

	callConn(t, svc.handleDeleteSource, map[string]any{
		"user_id": "u1", "space_id": "s1", "source_id": sources[0].ID,
	})

	after, _ := svc.ListSources(ctx, "u1", "s1")
	if len(after) != 0 {
		t.Errorf("after delete: got %d sources, want 0", len(after))
	}
}
