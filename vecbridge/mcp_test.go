package vecbridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/rand/v2"
	"testing"

	"github.com/hazyhaar/horosvec"
	"github.com/hazyhaar/pkg/dbopen"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	_ "modernc.org/sqlite"
)

var testMCPImpl = &mcp.Implementation{Name: "vecbridge-test", Version: "0.1.0"}

func testService(t *testing.T) *Service {
	t.Helper()
	db := dbopen.OpenMemory(t)
	svc, err := NewFromDB(db, horosvec.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func buildTestIndex(t *testing.T, svc *Service, dim, n int) ([][]float32, [][]byte) {
	t.Helper()
	vecs := make([][]float32, n)
	ids := make([][]byte, n)
	for i := range vecs {
		v := make([]float32, dim)
		for j := range v {
			v[j] = rand.Float32() - 0.5
		}
		vecs[i] = v
		ids[i] = []byte{byte(i >> 8), byte(i & 0xff)}
	}
	iter := &sliceIter{vecs: vecs, ids: ids}
	if err := svc.Index.Build(context.Background(), iter); err != nil {
		t.Fatal(err)
	}
	return vecs, ids
}

func mcpSession(t *testing.T, svc *Service) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(testMCPImpl, nil)
	svc.RegisterMCP(srv)

	serverT, clientT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(testMCPImpl, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args any) string {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if err := result.GetError(); err != nil {
		t.Fatalf("CallTool(%s) tool error: %v", name, err)
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s): expected TextContent", name)
	}
	return tc.Text
}

// --- horosvec_stats ---

func TestMCP_Stats_Empty(t *testing.T) {
	svc := testService(t)
	session := mcpSession(t, svc)

	text := callTool(t, session, "horosvec_stats", map[string]any{})

	var resp struct {
		Count        int  `json:"count"`
		NeedsRebuild bool `json:"needs_rebuild"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 0 {
		t.Errorf("Count = %d, want 0", resp.Count)
	}
}

// --- horosvec_insert + horosvec_search ---

func TestMCP_InsertAndSearch(t *testing.T) {
	svc := testService(t)

	// Must build index first with seed data (same dimension as test vectors).
	buildTestIndex(t, svc, 4, 10)

	session := mcpSession(t, svc)

	// Insert via MCP.
	ids := []string{hex.EncodeToString([]byte{0x00, 0x01}), hex.EncodeToString([]byte{0x00, 0x02})}
	vecs := [][]float32{
		{1.0, 0.0, 0.0, 0.0},
		{0.0, 1.0, 0.0, 0.0},
	}

	text := callTool(t, session, "horosvec_insert", map[string]any{
		"ids":     ids,
		"vectors": vecs,
	})

	var insertResp struct {
		Inserted int `json:"inserted"`
		Count    int `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &insertResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if insertResp.Inserted != 2 {
		t.Errorf("Inserted = %d, want 2", insertResp.Inserted)
	}
	// Count reflects the built index; live inserts may buffer separately.
	if insertResp.Count < 10 {
		t.Errorf("Count = %d, want >= 10", insertResp.Count)
	}

	// Search for the first vector.
	text = callTool(t, session, "horosvec_search", map[string]any{
		"vector": []float32{1.0, 0.0, 0.0, 0.0},
		"top_k":  2,
	})

	var searchResp struct {
		Results []struct {
			ID    string  `json:"id"`
			Score float64 `json:"score"`
		} `json:"results"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &searchResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if searchResp.Count == 0 {
		t.Fatal("expected search results")
	}
	// First result should be the vector we inserted (closest to query).
	if searchResp.Results[0].ID != ids[0] {
		t.Errorf("closest ID = %q, want %q", searchResp.Results[0].ID, ids[0])
	}
}

// --- horosvec_similar ---

func TestMCP_Similar(t *testing.T) {
	svc := testService(t)
	dim := 32
	_, ids := buildTestIndex(t, svc, dim, 50)
	session := mcpSession(t, svc)

	refID := hex.EncodeToString(ids[0])
	text := callTool(t, session, "horosvec_similar", map[string]any{
		"id":    refID,
		"top_k": 5,
	})

	var resp struct {
		Results []struct {
			ID    string  `json:"id"`
			Score float64 `json:"score"`
		} `json:"results"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count == 0 {
		t.Fatal("expected similar results")
	}
	// The reference vector itself should NOT appear in results.
	for _, r := range resp.Results {
		if r.ID == refID {
			t.Error("similar results should not include the reference ID")
		}
	}
}

// --- horosvec_stats after insert ---

func TestMCP_Stats_AfterBuild(t *testing.T) {
	svc := testService(t)
	buildTestIndex(t, svc, 16, 100)
	session := mcpSession(t, svc)

	text := callTool(t, session, "horosvec_stats", map[string]any{})

	var resp struct {
		Count        int  `json:"count"`
		NeedsRebuild bool `json:"needs_rebuild"`
	}
	json.Unmarshal([]byte(text), &resp)
	if resp.Count != 100 {
		t.Errorf("Count = %d, want 100", resp.Count)
	}
}
