package domkeeper

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/hazyhaar/chrc/domkeeper/internal/store"
	"github.com/hazyhaar/pkg/dbopen"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	_ "modernc.org/sqlite"
)

var testImpl = &mcp.Implementation{Name: "domkeeper-test", Version: "0.1.0"}

// testKeeper creates a Keeper backed by an in-memory SQLite database.
func testKeeper(t *testing.T) *Keeper {
	t.Helper()
	db := dbopen.OpenMemory(t)
	if _, err := db.Exec(store.Schema); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	s := &store.Store{DB: db}
	return &Keeper{
		store:  s,
		logger: slog.Default(),
		config: &Config{},
	}
}

// mcpSession creates a Keeper, registers MCP tools, and returns a connected
// client session that can call tools end-to-end.
func mcpSession(t *testing.T) (*Keeper, *mcp.ClientSession) {
	t.Helper()
	k := testKeeper(t)

	srv := mcp.NewServer(testImpl, nil)
	k.RegisterMCP(srv)

	serverT, clientT := mcp.NewInMemoryTransports()
	ctx := context.Background()

	go func() {
		_ = srv.Run(ctx, serverT)
	}()

	client := mcp.NewClient(testImpl, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	return k, session
}

// callTool invokes a tool and returns the JSON text from the first TextContent.
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
	if len(result.Content) == 0 {
		t.Fatalf("CallTool(%s): empty content", name)
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s): expected TextContent, got %T", name, result.Content[0])
	}
	return tc.Text
}

// --- domkeeper_add_rule ---

func TestMCP_AddRule(t *testing.T) {
	_, session := mcpSession(t)

	text := callTool(t, session, "domkeeper_add_rule", map[string]any{
		"name":         "Article extractor",
		"url_pattern":  "https://example.com/articles/*",
		"selectors":    []string{"article.main", "div.content"},
		"extract_mode": "css",
		"trust_level":  "official",
		"priority":     5,
	})

	var rule store.Rule
	if err := json.Unmarshal([]byte(text), &rule); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rule.ID == "" {
		t.Error("expected non-empty rule ID")
	}
	if rule.Name != "Article extractor" {
		t.Errorf("Name = %q, want %q", rule.Name, "Article extractor")
	}
	if rule.ExtractMode != "css" {
		t.Errorf("ExtractMode = %q, want %q", rule.ExtractMode, "css")
	}
	if rule.TrustLevel != "official" {
		t.Errorf("TrustLevel = %q, want %q", rule.TrustLevel, "official")
	}
	if rule.Priority != 5 {
		t.Errorf("Priority = %d, want 5", rule.Priority)
	}
	if len(rule.Selectors) != 2 {
		t.Errorf("Selectors len = %d, want 2", len(rule.Selectors))
	}
}

func TestMCP_AddRule_Defaults(t *testing.T) {
	_, session := mcpSession(t)

	text := callTool(t, session, "domkeeper_add_rule", map[string]any{
		"name":        "Minimal rule",
		"url_pattern": "https://example.com/*",
	})

	var rule store.Rule
	json.Unmarshal([]byte(text), &rule)
	if rule.ExtractMode != "auto" {
		t.Errorf("default ExtractMode = %q, want %q", rule.ExtractMode, "auto")
	}
	if rule.TrustLevel != "unverified" {
		t.Errorf("default TrustLevel = %q, want %q", rule.TrustLevel, "unverified")
	}
}

// --- domkeeper_list_rules ---

func TestMCP_ListRules(t *testing.T) {
	k, session := mcpSession(t)
	ctx := context.Background()

	k.AddRule(ctx, &store.Rule{
		ID: "r1", Name: "Rule 1", URLPattern: "*", ExtractMode: "auto",
		TrustLevel: "official", Enabled: true,
	})
	k.AddRule(ctx, &store.Rule{
		ID: "r2", Name: "Rule 2", URLPattern: "*", ExtractMode: "css",
		TrustLevel: "community", Enabled: false,
	})

	// List all.
	text := callTool(t, session, "domkeeper_list_rules", map[string]any{})
	var rules []*store.Rule
	if err := json.Unmarshal([]byte(text), &rules); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	// List enabled only.
	text = callTool(t, session, "domkeeper_list_rules", map[string]any{"enabled_only": true})
	json.Unmarshal([]byte(text), &rules)
	if len(rules) != 1 {
		t.Fatalf("expected 1 enabled rule, got %d", len(rules))
	}
	if rules[0].Name != "Rule 1" {
		t.Errorf("enabled rule = %q, want %q", rules[0].Name, "Rule 1")
	}
}

// --- domkeeper_delete_rule ---

func TestMCP_DeleteRule(t *testing.T) {
	k, session := mcpSession(t)
	ctx := context.Background()

	k.AddRule(ctx, &store.Rule{
		ID: "del-me", Name: "Delete me", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "unverified", Enabled: true,
	})

	text := callTool(t, session, "domkeeper_delete_rule", map[string]any{"rule_id": "del-me"})
	var resp map[string]string
	json.Unmarshal([]byte(text), &resp)
	if resp["status"] != "deleted" {
		t.Errorf("status = %q, want %q", resp["status"], "deleted")
	}

	r, _ := k.GetRule(ctx, "del-me")
	if r != nil {
		t.Error("rule should be deleted")
	}
}

// --- domkeeper_add_folder ---

func TestMCP_AddFolder(t *testing.T) {
	_, session := mcpSession(t)

	text := callTool(t, session, "domkeeper_add_folder", map[string]any{
		"name":        "Research",
		"description": "Academic papers",
	})

	var folder store.Folder
	if err := json.Unmarshal([]byte(text), &folder); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if folder.ID == "" {
		t.Error("expected non-empty folder ID")
	}
	if folder.Name != "Research" {
		t.Errorf("Name = %q, want %q", folder.Name, "Research")
	}
}

// --- domkeeper_list_folders ---

func TestMCP_ListFolders(t *testing.T) {
	k, session := mcpSession(t)
	ctx := context.Background()

	k.AddFolder(ctx, &store.Folder{ID: "f1", Name: "Alpha"})
	k.AddFolder(ctx, &store.Folder{ID: "f2", Name: "Beta"})

	text := callTool(t, session, "domkeeper_list_folders", map[string]any{})
	var folders []*store.Folder
	json.Unmarshal([]byte(text), &folders)
	if len(folders) != 2 {
		t.Fatalf("expected 2 folders, got %d", len(folders))
	}
}

// --- domkeeper_stats ---

func TestMCP_Stats(t *testing.T) {
	k, session := mcpSession(t)
	ctx := context.Background()

	// Empty stats.
	text := callTool(t, session, "domkeeper_stats", map[string]any{})
	var stats Stats
	if err := json.Unmarshal([]byte(text), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stats.Rules != 0 || stats.Folders != 0 || stats.Content != 0 || stats.Chunks != 0 {
		t.Errorf("expected all zeros, got %+v", stats)
	}

	// Add data and re-check.
	k.AddRule(ctx, &store.Rule{
		ID: "r1", Name: "test", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "official", Enabled: true,
	})
	k.AddFolder(ctx, &store.Folder{ID: "f1", Name: "Folder"})

	text = callTool(t, session, "domkeeper_stats", map[string]any{})
	json.Unmarshal([]byte(text), &stats)
	if stats.Rules != 1 {
		t.Errorf("Rules = %d, want 1", stats.Rules)
	}
	if stats.Folders != 1 {
		t.Errorf("Folders = %d, want 1", stats.Folders)
	}
}

// --- domkeeper_search ---

func TestMCP_Search(t *testing.T) {
	k, session := mcpSession(t)
	ctx := context.Background()

	k.AddRule(ctx, &store.Rule{
		ID: "r1", Name: "test", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "official", Enabled: true,
	})
	k.store.InsertContent(ctx, &store.Content{
		ID: "c1", RuleID: "r1", PageURL: "https://example.com",
		ContentHash: "h1", ExtractedText: "test content",
		Title: "Test Page", TrustLevel: "official",
	})
	k.store.InsertChunks(ctx, []*store.Chunk{
		{ID: "ch1", ContentID: "c1", ChunkIndex: 0, Text: "Quantum computing is revolutionary", TokenCount: 5},
	})

	text := callTool(t, session, "domkeeper_search", map[string]any{
		"query": "quantum computing",
		"limit": 10,
	})

	var results []*store.SearchResult
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	if results[0].ContentTitle != "Test Page" {
		t.Errorf("title = %q, want %q", results[0].ContentTitle, "Test Page")
	}
}

func TestMCP_Search_Empty(t *testing.T) {
	_, session := mcpSession(t)

	text := callTool(t, session, "domkeeper_search", map[string]any{
		"query": "nonexistent query",
	})

	var results []*store.SearchResult
	json.Unmarshal([]byte(text), &results)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// --- domkeeper_get_content ---

func TestMCP_GetContent(t *testing.T) {
	k, session := mcpSession(t)
	ctx := context.Background()

	k.AddRule(ctx, &store.Rule{
		ID: "r1", Name: "test", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "official", Enabled: true,
	})
	k.store.InsertContent(ctx, &store.Content{
		ID: "c1", RuleID: "r1", PageURL: "https://example.com",
		ContentHash: "h1", ExtractedText: "Full text here",
		Title: "Page Title", TrustLevel: "official",
	})
	k.store.InsertChunks(ctx, []*store.Chunk{
		{ID: "ch1", ContentID: "c1", ChunkIndex: 0, Text: "Chunk zero", TokenCount: 2},
		{ID: "ch2", ContentID: "c1", ChunkIndex: 1, Text: "Chunk one", TokenCount: 2},
	})

	text := callTool(t, session, "domkeeper_get_content", map[string]any{"content_id": "c1"})

	var resp struct {
		Content *store.Content `json:"content"`
		Chunks  []*store.Chunk `json:"chunks"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Content == nil {
		t.Fatal("expected content")
	}
	if resp.Content.Title != "Page Title" {
		t.Errorf("Title = %q, want %q", resp.Content.Title, "Page Title")
	}
	if len(resp.Chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(resp.Chunks))
	}
}

func TestMCP_GetContent_NotFound(t *testing.T) {
	_, session := mcpSession(t)

	text := callTool(t, session, "domkeeper_get_content", map[string]any{"content_id": "nonexistent"})

	var resp map[string]string
	json.Unmarshal([]byte(text), &resp)
	if resp["error"] != "content not found" {
		t.Errorf("expected 'content not found', got %q", resp["error"])
	}
}

// --- domkeeper_premium_search ---

func TestMCP_PremiumSearch_Free(t *testing.T) {
	k, session := mcpSession(t)
	ctx := context.Background()

	k.AddRule(ctx, &store.Rule{
		ID: "r1", Name: "test", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "official", Enabled: true,
	})
	k.store.InsertContent(ctx, &store.Content{
		ID: "c1", RuleID: "r1", PageURL: "https://example.com",
		ContentHash: "h1", ExtractedText: "test",
		Title: "Test", TrustLevel: "official",
	})
	k.store.InsertChunks(ctx, []*store.Chunk{
		{ID: "ch1", ContentID: "c1", ChunkIndex: 0, Text: "Machine learning algorithms", TokenCount: 3},
	})

	text := callTool(t, session, "domkeeper_premium_search", map[string]any{
		"query": "machine learning",
		"tier":  "free",
	})

	var result PremiumSearchResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Tier != TierFree {
		t.Errorf("Tier = %q, want %q", result.Tier, TierFree)
	}
	if result.TotalPasses != 1 {
		t.Errorf("TotalPasses = %d, want 1", result.TotalPasses)
	}
}

func TestMCP_PremiumSearch_Premium(t *testing.T) {
	k, session := mcpSession(t)
	ctx := context.Background()

	k.AddRule(ctx, &store.Rule{
		ID: "r1", Name: "test", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "official", Enabled: true,
	})
	k.store.InsertContent(ctx, &store.Content{
		ID: "c1", RuleID: "r1", PageURL: "https://example.com",
		ContentHash: "h1", ExtractedText: "data science and analytics tools",
		Title: "Data Science", TrustLevel: "official",
	})
	k.store.InsertChunks(ctx, []*store.Chunk{
		{ID: "ch1", ContentID: "c1", ChunkIndex: 0, Text: "data science and analytics tools for researchers", TokenCount: 8},
	})

	text := callTool(t, session, "domkeeper_premium_search", map[string]any{
		"query":      "data science analytics",
		"tier":       "premium",
		"max_passes": 3,
	})

	var result PremiumSearchResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Tier != TierPremium {
		t.Errorf("Tier = %q, want %q", result.Tier, TierPremium)
	}
	if result.TotalPasses < 1 {
		t.Errorf("TotalPasses = %d, want >= 1", result.TotalPasses)
	}
}

// --- domkeeper_gpu_stats ---

func TestMCP_GPUStats(t *testing.T) {
	_, session := mcpSession(t)

	text := callTool(t, session, "domkeeper_gpu_stats", map[string]any{})

	var stats GPUStatsResult
	if err := json.Unmarshal([]byte(text), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Empty DB: pricing may be nil or empty slice, threshold nil — no error is success.
}

// --- domkeeper_gpu_threshold ---

func TestMCP_GPUThreshold(t *testing.T) {
	_, session := mcpSession(t)

	text := callTool(t, session, "domkeeper_gpu_threshold", map[string]any{
		"backlog_units": 100,
	})

	var threshold store.GPUThreshold
	if err := json.Unmarshal([]byte(text), &threshold); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if threshold.BacklogUnits != 100 {
		t.Errorf("BacklogUnits = %d, want 100", threshold.BacklogUnits)
	}
}
