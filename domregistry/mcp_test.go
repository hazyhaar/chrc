package domregistry

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/hazyhaar/chrc/domregistry/internal/store"
	"github.com/hazyhaar/pkg/dbopen"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	_ "modernc.org/sqlite"
)

var testImpl = &mcp.Implementation{Name: "domregistry-test", Version: "0.1.0"}

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	db := dbopen.OpenMemory(t)
	if _, err := db.Exec(store.Schema); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	s := &store.Store{DB: db}
	return &Registry{
		store:  s,
		logger: slog.Default(),
		config: &Config{AutoAccept: true, DegradedThreshold: 0.5},
	}
}

func mcpSession(t *testing.T) (*Registry, *mcp.ClientSession) {
	t.Helper()
	r := testRegistry(t)

	srv := mcp.NewServer(testImpl, nil)
	r.RegisterMCP(srv)

	serverT, clientT := mcp.NewInMemoryTransports()
	ctx := context.Background()

	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(testImpl, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return r, session
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
	if len(result.Content) == 0 {
		t.Fatalf("CallTool(%s): empty content", name)
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s): expected TextContent, got %T", name, result.Content[0])
	}
	return tc.Text
}

// --- domregistry_publish_profile ---

func TestMCP_PublishProfile(t *testing.T) {
	_, session := mcpSession(t)

	text := callTool(t, session, "domregistry_publish_profile", map[string]any{
		"url_pattern": "https://example.com/articles/*",
		"domain":      "example.com",
		"extractors":  `{"article": "div.content"}`,
		"dom_profile": `{"landmarks": ["nav", "main"]}`,
		"trust_level": "official",
		"instance_id": "inst-001",
	})

	var p store.Profile
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.ID == "" {
		t.Error("expected non-empty profile ID")
	}
	if p.Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", p.Domain, "example.com")
	}
	if p.TrustLevel != "official" {
		t.Errorf("TrustLevel = %q, want %q", p.TrustLevel, "official")
	}
}

func TestMCP_PublishProfile_DefaultTrust(t *testing.T) {
	_, session := mcpSession(t)

	text := callTool(t, session, "domregistry_publish_profile", map[string]any{
		"url_pattern": "https://test.com/*",
		"domain":      "test.com",
		"extractors":  `{}`,
		"dom_profile": `{}`,
	})

	var p store.Profile
	json.Unmarshal([]byte(text), &p)
	if p.TrustLevel != "community" {
		t.Errorf("default TrustLevel = %q, want %q", p.TrustLevel, "community")
	}
}

// --- domregistry_search_profiles ---

func TestMCP_SearchProfiles(t *testing.T) {
	r, session := mcpSession(t)
	ctx := context.Background()

	r.PublishProfile(ctx, &store.Profile{
		ID: "p1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{"a":"b"}`, DOMProfile: `{}`, TrustLevel: "official",
	})
	r.PublishProfile(ctx, &store.Profile{
		ID: "p2", URLPattern: "https://other.com/*", Domain: "other.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "community",
	})

	text := callTool(t, session, "domregistry_search_profiles", map[string]any{
		"domain": "example.com",
	})

	var profiles []*store.Profile
	if err := json.Unmarshal([]byte(text), &profiles); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", profiles[0].Domain, "example.com")
	}
}

// --- domregistry_submit_correction ---

func TestMCP_SubmitCorrection(t *testing.T) {
	r, session := mcpSession(t)
	ctx := context.Background()

	r.PublishProfile(ctx, &store.Profile{
		ID: "p1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{"old":"selector"}`, DOMProfile: `{}`, TrustLevel: "community",
	})

	text := callTool(t, session, "domregistry_submit_correction", map[string]any{
		"profile_id":     "p1",
		"instance_id":    "inst-001",
		"old_extractors": `{"old":"selector"}`,
		"new_extractors": `{"new":"selector"}`,
		"reason":         "layout_change",
	})

	var c store.Correction
	if err := json.Unmarshal([]byte(text), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.ID == "" {
		t.Error("expected non-empty correction ID")
	}
	if c.ProfileID != "p1" {
		t.Errorf("ProfileID = %q, want %q", c.ProfileID, "p1")
	}
	if c.Reason != "layout_change" {
		t.Errorf("Reason = %q, want %q", c.Reason, "layout_change")
	}
}

// --- domregistry_report_failure ---

func TestMCP_ReportFailure(t *testing.T) {
	r, session := mcpSession(t)
	ctx := context.Background()

	r.PublishProfile(ctx, &store.Profile{
		ID: "p1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "community",
	})

	text := callTool(t, session, "domregistry_report_failure", map[string]any{
		"profile_id":  "p1",
		"instance_id": "inst-001",
		"error_type":  "selector_broken",
		"message":     "CSS selector .content not found",
	})

	var resp map[string]string
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "reported" {
		t.Errorf("status = %q, want %q", resp["status"], "reported")
	}
	if resp["report_id"] == "" {
		t.Error("expected non-empty report_id")
	}
}

// --- domregistry_leaderboard ---

func TestMCP_Leaderboard_Domain(t *testing.T) {
	r, session := mcpSession(t)
	ctx := context.Background()

	r.PublishProfile(ctx, &store.Profile{
		ID: "p1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "official",
	})

	text := callTool(t, session, "domregistry_leaderboard", map[string]any{
		"type":  "domain",
		"limit": 10,
	})

	var entries []*store.LeaderboardEntry
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// At least one entry expected for example.com.
	if len(entries) == 0 {
		t.Fatal("expected at least 1 leaderboard entry")
	}
}

func TestMCP_Leaderboard_Instance(t *testing.T) {
	_, session := mcpSession(t)

	text := callTool(t, session, "domregistry_leaderboard", map[string]any{
		"type":  "instance",
		"limit": 10,
	})

	var reps []*store.InstanceReputation
	if err := json.Unmarshal([]byte(text), &reps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Empty is fine — no instances yet.
}

// --- domregistry_stats ---

func TestMCP_Stats(t *testing.T) {
	r, session := mcpSession(t)
	ctx := context.Background()

	// Empty stats.
	text := callTool(t, session, "domregistry_stats", map[string]any{})
	var stats Stats
	if err := json.Unmarshal([]byte(text), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stats.Profiles != 0 {
		t.Errorf("Profiles = %d, want 0", stats.Profiles)
	}

	// Add a profile and re-check.
	r.PublishProfile(ctx, &store.Profile{
		ID: "p1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "official",
	})

	text = callTool(t, session, "domregistry_stats", map[string]any{})
	json.Unmarshal([]byte(text), &stats)
	if stats.Profiles != 1 {
		t.Errorf("Profiles = %d, want 1", stats.Profiles)
	}
}
