package store

import (
	"context"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/hazyhaar/pkg/dbopen"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db := dbopen.OpenMemory(t)
	if _, err := db.Exec(Schema); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return &Store{DB: db}
}

func TestProfileCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p := &Profile{
		ID:           "prof-1",
		URLPattern:   "https://example.com/articles/*",
		Domain:       "example.com",
		Extractors:   `{"strategy":"css","selectors":{"title":"h1","body":"article"}}`,
		DOMProfile:   `{"landmarks":[{"tag":"main","xpath":"/html/body/main"}]}`,
		TrustLevel:   "community",
		SuccessRate:  0.95,
		Contributors: []string{"inst-1"},
	}
	if err := s.InsertProfile(ctx, p); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Get by ID.
	got, err := s.GetProfile(ctx, "prof-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("get: got nil")
	}
	if got.Domain != "example.com" {
		t.Errorf("Domain: got %q, want %q", got.Domain, "example.com")
	}
	if got.SuccessRate != 0.95 {
		t.Errorf("SuccessRate: got %f, want 0.95", got.SuccessRate)
	}
	if len(got.Contributors) != 1 || got.Contributors[0] != "inst-1" {
		t.Errorf("Contributors: got %v, want [inst-1]", got.Contributors)
	}

	// Get by pattern.
	got2, err := s.GetProfileByPattern(ctx, "https://example.com/articles/*")
	if err != nil {
		t.Fatalf("get by pattern: %v", err)
	}
	if got2 == nil || got2.ID != "prof-1" {
		t.Error("get by pattern: wrong result")
	}

	// List by domain.
	profiles, err := s.ListProfilesByDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("list by domain: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("list by domain: got %d, want 1", len(profiles))
	}

	// Update.
	got.SuccessRate = 0.99
	if err := s.UpdateProfile(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got3, _ := s.GetProfile(ctx, "prof-1")
	if got3.SuccessRate != 0.99 {
		t.Errorf("SuccessRate after update: got %f, want 0.99", got3.SuccessRate)
	}

	// Delete.
	if err := s.DeleteProfile(ctx, "prof-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got4, _ := s.GetProfile(ctx, "prof-1")
	if got4 != nil {
		t.Error("get after delete: expected nil")
	}
}

func TestProfileSuccessRate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p := &Profile{
		ID:          "prof-1",
		URLPattern:  "https://example.com/*",
		Domain:      "example.com",
		Extractors:  `{}`,
		DOMProfile:  `{}`,
		TrustLevel:  "community",
		SuccessRate: 1.0,
	}
	s.InsertProfile(ctx, p)

	// Record failure: EMA should decrease rate.
	if err := s.RecordFailure(ctx, "prof-1"); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	got, _ := s.GetProfile(ctx, "prof-1")
	if got.SuccessRate >= 1.0 {
		t.Errorf("success_rate after failure should decrease: got %f", got.SuccessRate)
	}

	// Record success: EMA should increase rate.
	prev := got.SuccessRate
	if err := s.RecordSuccess(ctx, "prof-1"); err != nil {
		t.Fatalf("record success: %v", err)
	}
	got2, _ := s.GetProfile(ctx, "prof-1")
	if got2.SuccessRate <= prev {
		t.Errorf("success_rate after success should increase: got %f, prev %f", got2.SuccessRate, prev)
	}
}

func TestAddContributor(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p := &Profile{
		ID: "prof-1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "community",
		Contributors: []string{"inst-1"},
	}
	s.InsertProfile(ctx, p)

	// Add new contributor.
	if err := s.AddContributor(ctx, "prof-1", "inst-2"); err != nil {
		t.Fatalf("add contributor: %v", err)
	}
	got, _ := s.GetProfile(ctx, "prof-1")
	if len(got.Contributors) != 2 {
		t.Fatalf("contributors: got %d, want 2", len(got.Contributors))
	}

	// Adding same contributor is idempotent.
	if err := s.AddContributor(ctx, "prof-1", "inst-2"); err != nil {
		t.Fatalf("add contributor dup: %v", err)
	}
	got2, _ := s.GetProfile(ctx, "prof-1")
	if len(got2.Contributors) != 2 {
		t.Errorf("contributors after dup: got %d, want 2", len(got2.Contributors))
	}
}

func TestCorrectionWorkflow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Set up profile.
	p := &Profile{
		ID: "prof-1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{"old":true}`, DOMProfile: `{}`, TrustLevel: "community",
	}
	s.InsertProfile(ctx, p)

	// Submit correction.
	c := &Correction{
		ID:            "corr-1",
		ProfileID:     "prof-1",
		InstanceID:    "inst-1",
		OldExtractors: `{"old":true}`,
		NewExtractors: `{"new":true}`,
		Reason:        "selector_broken",
	}
	if err := s.InsertCorrection(ctx, c); err != nil {
		t.Fatalf("insert correction: %v", err)
	}

	// Verify pending.
	pending, err := s.ListPendingCorrections(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending: got %d, want 1", len(pending))
	}

	// Score: new instance should NOT be auto-accepted.
	autoAccept, err := s.ScoreCorrection(ctx, "corr-1")
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if autoAccept {
		t.Error("new instance should not be auto-accepted")
	}

	// Accept correction.
	if err := s.AcceptCorrection(ctx, "corr-1"); err != nil {
		t.Fatalf("accept: %v", err)
	}

	// Verify profile updated.
	updated, _ := s.GetProfile(ctx, "prof-1")
	if updated.Extractors != `{"new":true}` {
		t.Errorf("extractors after accept: got %q, want %q", updated.Extractors, `{"new":true}`)
	}
	if updated.TotalRepairs != 1 {
		t.Errorf("total_repairs: got %d, want 1", updated.TotalRepairs)
	}

	// Verify reputation updated.
	rep, _ := s.GetReputation(ctx, "inst-1")
	if rep == nil {
		t.Fatal("reputation: got nil")
	}
	if rep.CorrectionsAccepted != 1 {
		t.Errorf("corrections_accepted: got %d, want 1", rep.CorrectionsAccepted)
	}
}

func TestCorrectionReject(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p := &Profile{
		ID: "prof-1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{"old":true}`, DOMProfile: `{}`, TrustLevel: "community",
	}
	s.InsertProfile(ctx, p)

	c := &Correction{
		ID: "corr-1", ProfileID: "prof-1", InstanceID: "inst-1",
		OldExtractors: `{"old":true}`, NewExtractors: `{"bad":true}`, Reason: "new_field",
	}
	s.InsertCorrection(ctx, c)

	if err := s.RejectCorrection(ctx, "corr-1"); err != nil {
		t.Fatalf("reject: %v", err)
	}

	// Profile should NOT be updated.
	got, _ := s.GetProfile(ctx, "prof-1")
	if got.Extractors != `{"old":true}` {
		t.Errorf("extractors should not change: got %q", got.Extractors)
	}

	// Reputation should reflect rejection.
	rep, _ := s.GetReputation(ctx, "inst-1")
	if rep.CorrectionsRejected != 1 {
		t.Errorf("corrections_rejected: got %d, want 1", rep.CorrectionsRejected)
	}
}

func TestAutoAcceptScoring(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p := &Profile{
		ID: "prof-1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "community",
	}
	s.InsertProfile(ctx, p)

	// Build up instance reputation: 3 accepted, 0 rejected.
	for i := 0; i < 3; i++ {
		c := &Correction{
			ID: "setup-" + string(rune('a'+i)), ProfileID: "prof-1", InstanceID: "inst-good",
			OldExtractors: `{}`, NewExtractors: `{}`, Reason: "layout_change",
		}
		s.InsertCorrection(ctx, c)
		s.AcceptCorrection(ctx, c.ID)
	}

	// Now a new correction should be auto-acceptable.
	c := &Correction{
		ID: "corr-new", ProfileID: "prof-1", InstanceID: "inst-good",
		OldExtractors: `{}`, NewExtractors: `{"auto":true}`, Reason: "selector_broken",
	}
	s.InsertCorrection(ctx, c)

	autoAccept, err := s.ScoreCorrection(ctx, "corr-new")
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if !autoAccept {
		t.Error("instance with 3 accepted / 0 rejected should be auto-accepted")
	}
}

func TestReport(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p := &Profile{
		ID: "prof-1", URLPattern: "https://example.com/*", Domain: "example.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "community", SuccessRate: 1.0,
	}
	s.InsertProfile(ctx, p)

	r := &Report{
		ID:         "rpt-1",
		ProfileID:  "prof-1",
		InstanceID: "inst-1",
		ErrorType:  "selector_broken",
		Message:    "main selector returns empty",
	}
	if err := s.InsertReport(ctx, r); err != nil {
		t.Fatalf("insert report: %v", err)
	}

	// Success rate should decrease.
	got, _ := s.GetProfile(ctx, "prof-1")
	if got.SuccessRate >= 1.0 {
		t.Errorf("success_rate after report should decrease: got %f", got.SuccessRate)
	}

	// Count reports.
	n, err := s.CountReports(ctx)
	if err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if n != 1 {
		t.Errorf("report count: got %d, want 1", n)
	}
}

func TestDomainLeaderboard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Two profiles for same domain, one for another.
	s.InsertProfile(ctx, &Profile{
		ID: "p1", URLPattern: "https://a.com/*", Domain: "a.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "community",
		SuccessRate: 0.9, TotalUses: 100,
	})
	s.InsertProfile(ctx, &Profile{
		ID: "p2", URLPattern: "https://a.com/blog/*", Domain: "a.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "community",
		SuccessRate: 0.8, TotalUses: 50,
	})
	s.InsertProfile(ctx, &Profile{
		ID: "p3", URLPattern: "https://b.com/*", Domain: "b.com",
		Extractors: `{}`, DOMProfile: `{}`, TrustLevel: "official",
		SuccessRate: 0.5, TotalUses: 200,
	})

	entries, err := s.DomainLeaderboard(ctx, 10)
	if err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("leaderboard: got %d entries, want 2", len(entries))
	}
	// a.com should rank first (avg 0.85 > 0.5).
	if entries[0].Domain != "a.com" {
		t.Errorf("first domain: got %q, want %q", entries[0].Domain, "a.com")
	}
	if entries[0].ProfileCount != 2 {
		t.Errorf("profile_count for a.com: got %d, want 2", entries[0].ProfileCount)
	}
}
