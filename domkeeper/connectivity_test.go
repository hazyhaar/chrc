package domkeeper

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hazyhaar/chrc/domkeeper/internal/store"
	"github.com/hazyhaar/pkg/connectivity"
	"github.com/hazyhaar/pkg/dbopen"

	_ "modernc.org/sqlite"
)

func testKeeperConn(t *testing.T) (*Keeper, *connectivity.Router) {
	t.Helper()
	db := dbopen.OpenMemory(t)
	if _, err := db.Exec(store.Schema); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	k := &Keeper{store: &store.Store{DB: db}, config: &Config{}}
	router := connectivity.New()
	k.RegisterConnectivity(router)
	return k, router
}

func TestConn_Search(t *testing.T) {
	k, router := testKeeperConn(t)
	ctx := context.Background()

	k.AddRule(ctx, &store.Rule{
		ID: "r1", Name: "test", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "official", Enabled: true,
	})
	k.store.InsertContent(ctx, &store.Content{
		ID: "c1", RuleID: "r1", PageURL: "https://example.com",
		ContentHash: "h1", ExtractedText: "test", Title: "Page",
		TrustLevel: "official",
	})
	k.store.InsertChunks(ctx, []*store.Chunk{
		{ID: "ch1", ContentID: "c1", ChunkIndex: 0, Text: "Neural network training", TokenCount: 3},
	})

	payload, _ := json.Marshal(map[string]any{"query": "neural network", "limit": 5})
	resp, err := router.Call(ctx, "domkeeper_search", payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var results []*store.SearchResult
	if err := json.Unmarshal(resp, &results); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
}

func TestConn_AddRule(t *testing.T) {
	_, router := testKeeperConn(t)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]any{
		"name": "Test rule", "url_pattern": "https://example.com/*",
	})
	resp, err := router.Call(ctx, "domkeeper_add_rule", payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var rule store.Rule
	json.Unmarshal(resp, &rule)
	if rule.ID == "" {
		t.Error("expected rule ID")
	}
	if rule.ExtractMode != "auto" {
		t.Errorf("default mode = %q, want auto", rule.ExtractMode)
	}
}

func TestConn_ListRules(t *testing.T) {
	k, router := testKeeperConn(t)
	ctx := context.Background()

	k.AddRule(ctx, &store.Rule{
		ID: "r1", Name: "A", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "official", Enabled: true,
	})

	resp, err := router.Call(ctx, "domkeeper_list_rules", []byte(`{}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var rules []*store.Rule
	json.Unmarshal(resp, &rules)
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
}

func TestConn_DeleteRule(t *testing.T) {
	k, router := testKeeperConn(t)
	ctx := context.Background()

	k.AddRule(ctx, &store.Rule{
		ID: "del-me", Name: "Del", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "unverified", Enabled: true,
	})

	resp, err := router.Call(ctx, "domkeeper_delete_rule", []byte(`{"rule_id":"del-me"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var result map[string]string
	json.Unmarshal(resp, &result)
	if result["status"] != "deleted" {
		t.Errorf("status = %q", result["status"])
	}
}

func TestConn_Stats(t *testing.T) {
	_, router := testKeeperConn(t)
	ctx := context.Background()

	resp, err := router.Call(ctx, "domkeeper_stats", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var stats Stats
	json.Unmarshal(resp, &stats)
	if stats.Rules != 0 {
		t.Errorf("Rules = %d, want 0", stats.Rules)
	}
}

func TestConn_PremiumSearch(t *testing.T) {
	k, router := testKeeperConn(t)
	ctx := context.Background()

	k.AddRule(ctx, &store.Rule{
		ID: "r1", Name: "test", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "official", Enabled: true,
	})
	k.store.InsertContent(ctx, &store.Content{
		ID: "c1", RuleID: "r1", PageURL: "https://example.com",
		ContentHash: "h1", ExtractedText: "test", Title: "Page",
		TrustLevel: "official",
	})
	k.store.InsertChunks(ctx, []*store.Chunk{
		{ID: "ch1", ContentID: "c1", ChunkIndex: 0, Text: "Deep learning models", TokenCount: 3},
	})

	payload, _ := json.Marshal(map[string]any{
		"query": "deep learning", "tier": "free",
	})
	resp, err := router.Call(ctx, "domkeeper_premium_search", payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var result PremiumSearchResult
	json.Unmarshal(resp, &result)
	if result.Tier != TierFree {
		t.Errorf("Tier = %q, want free", result.Tier)
	}
}

func TestConn_GPUStats(t *testing.T) {
	_, router := testKeeperConn(t)
	resp, err := router.Call(context.Background(), "domkeeper_gpu_stats", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var stats GPUStatsResult
	json.Unmarshal(resp, &stats)
	// No error means success — empty results are valid.
}

func TestConn_GPUThreshold(t *testing.T) {
	_, router := testKeeperConn(t)
	payload, _ := json.Marshal(map[string]any{"backlog_units": 50})
	resp, err := router.Call(context.Background(), "domkeeper_gpu_threshold", payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var threshold store.GPUThreshold
	json.Unmarshal(resp, &threshold)
	if threshold.BacklogUnits != 50 {
		t.Errorf("BacklogUnits = %d, want 50", threshold.BacklogUnits)
	}
}
