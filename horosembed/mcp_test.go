package horosembed

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var testMCPImpl = &mcp.Implementation{Name: "horosembed-test", Version: "0.1.0"}

func mcpSession(t *testing.T, emb Embedder) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(testMCPImpl, nil)
	RegisterMCP(srv, emb)

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

func mcpCallTool(t *testing.T, session *mcp.ClientSession, name string, args any) string {
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

func TestMCP_Embed(t *testing.T) {
	emb := New(Config{Dimension: 128, Model: "test-noop"})
	session := mcpSession(t, emb)

	text := mcpCallTool(t, session, "horosembed_embed", map[string]any{
		"text": "What is photosynthesis?",
	})

	var resp struct {
		Vector    []float32 `json:"vector"`
		Dimension int       `json:"dimension"`
		Model     string    `json:"model"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Dimension != 128 {
		t.Errorf("Dimension = %d, want 128", resp.Dimension)
	}
	if len(resp.Vector) != 128 {
		t.Errorf("vector len = %d, want 128", len(resp.Vector))
	}
	if resp.Model != "test-noop" {
		t.Errorf("Model = %q, want %q", resp.Model, "test-noop")
	}
}

func TestMCP_Batch(t *testing.T) {
	emb := New(Config{Dimension: 64, Model: "test-batch"})
	session := mcpSession(t, emb)

	text := mcpCallTool(t, session, "horosembed_batch", map[string]any{
		"texts": []string{"alpha", "beta", "gamma"},
	})

	var resp struct {
		Vectors   [][]float32 `json:"vectors"`
		Count     int         `json:"count"`
		Dimension int         `json:"dimension"`
		Model     string      `json:"model"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 3 {
		t.Errorf("Count = %d, want 3", resp.Count)
	}
	if resp.Dimension != 64 {
		t.Errorf("Dimension = %d, want 64", resp.Dimension)
	}
	if len(resp.Vectors) != 3 {
		t.Fatalf("vectors len = %d, want 3", len(resp.Vectors))
	}
	for i, v := range resp.Vectors {
		if len(v) != 64 {
			t.Errorf("vectors[%d] len = %d, want 64", i, len(v))
		}
	}
}
