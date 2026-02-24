package domregistry

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/hazyhaar/chrc/domregistry/internal/store"
	"github.com/hazyhaar/pkg/connectivity"
	"github.com/hazyhaar/pkg/dbopen"

	_ "modernc.org/sqlite"
)

func testRegistryConn(t *testing.T) (*Registry, *connectivity.Router) {
	t.Helper()
	db := dbopen.OpenMemory(t)
	if _, err := db.Exec(store.Schema); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	r := &Registry{
		store:  &store.Store{DB: db},
		logger: slog.Default(),
		config: &Config{AutoAccept: true, DegradedThreshold: 0.5},
	}
	router := connectivity.New()
	r.RegisterConnectivity(router)
	return r, router
}

func TestConn_SearchProfiles(t *testing.T) {
	r, router := testRegistryConn(t)
	ctx := context.Background()

	r.PublishProfile(ctx, &store.Profile{
		ID: "p1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "official",
	})

	payload, _ := json.Marshal(map[string]any{"domain": "example.com"})
	resp, err := router.Call(ctx, "domregistry_search_profiles", payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var profiles []*store.Profile
	json.Unmarshal(resp, &profiles)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
}

func TestConn_GetProfile(t *testing.T) {
	r, router := testRegistryConn(t)
	ctx := context.Background()

	r.PublishProfile(ctx, &store.Profile{
		ID: "p1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{"a":"b"}`, DOMProfile: `{}`, TrustLevel: "official",
	})

	payload, _ := json.Marshal(map[string]any{"id": "p1"})
	resp, err := router.Call(ctx, "domregistry_get_profile", payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var p store.Profile
	json.Unmarshal(resp, &p)
	if p.ID != "p1" {
		t.Errorf("ID = %q, want %q", p.ID, "p1")
	}
}

func TestConn_GetProfile_ByPattern(t *testing.T) {
	r, router := testRegistryConn(t)
	ctx := context.Background()

	r.PublishProfile(ctx, &store.Profile{
		ID: "p1", URLPattern: "https://test.com/*", Domain: "test.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "community",
	})

	payload, _ := json.Marshal(map[string]any{"url_pattern": "https://test.com/*"})
	resp, err := router.Call(ctx, "domregistry_get_profile", payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var p store.Profile
	json.Unmarshal(resp, &p)
	if p.Domain != "test.com" {
		t.Errorf("Domain = %q, want %q", p.Domain, "test.com")
	}
}

func TestConn_PublishProfile(t *testing.T) {
	_, router := testRegistryConn(t)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]any{
		"url_pattern": "https://new.com/*", "domain": "new.com",
		"extractors": `{}`, "dom_profile": `{}`, "trust_level": "official",
	})
	resp, err := router.Call(ctx, "domregistry_publish_profile", payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var p store.Profile
	json.Unmarshal(resp, &p)
	if p.ID == "" {
		t.Error("expected non-empty profile ID")
	}
}

func TestConn_SubmitCorrection(t *testing.T) {
	r, router := testRegistryConn(t)
	ctx := context.Background()

	r.PublishProfile(ctx, &store.Profile{
		ID: "p1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "community",
	})

	payload, _ := json.Marshal(map[string]any{
		"profile_id": "p1", "instance_id": "inst-001",
		"new_extractors": `{"fixed":"selector"}`, "reason": "selector_broken",
	})
	resp, err := router.Call(ctx, "domregistry_submit_correction", payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var c store.Correction
	json.Unmarshal(resp, &c)
	if c.ProfileID != "p1" {
		t.Errorf("ProfileID = %q, want p1", c.ProfileID)
	}
}

func TestConn_ReportFailure(t *testing.T) {
	r, router := testRegistryConn(t)
	ctx := context.Background()

	r.PublishProfile(ctx, &store.Profile{
		ID: "p1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "community",
	})

	payload, _ := json.Marshal(map[string]any{
		"profile_id": "p1", "instance_id": "inst-001",
		"error_type": "timeout", "message": "page load timeout",
	})
	resp, err := router.Call(ctx, "domregistry_report_failure", payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var result map[string]string
	json.Unmarshal(resp, &result)
	if result["status"] != "reported" {
		t.Errorf("status = %q, want reported", result["status"])
	}
}

func TestConn_Leaderboard(t *testing.T) {
	_, router := testRegistryConn(t)
	ctx := context.Background()

	resp, err := router.Call(ctx, "domregistry_leaderboard", []byte(`{"type":"domain","limit":10}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var entries []*store.LeaderboardEntry
	json.Unmarshal(resp, &entries)
	// Empty is valid.
}

func TestConn_Stats(t *testing.T) {
	_, router := testRegistryConn(t)
	resp, err := router.Call(context.Background(), "domregistry_stats", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var stats Stats
	json.Unmarshal(resp, &stats)
	if stats.Profiles != 0 {
		t.Errorf("Profiles = %d, want 0", stats.Profiles)
	}
}
