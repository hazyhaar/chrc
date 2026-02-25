package repair

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/hazyhaar/chrc/veille/internal/store"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
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
	return db
}

func TestTryRepair_Backoff(t *testing.T) {
	// WHAT: Server error triggers backoff (fetch_interval doubles).
	// WHY: Avoids hammering temporarily broken servers.
	db := openTestDB(t)
	st := store.NewStore(db)
	ctx := context.Background()

	src := &store.Source{
		ID: "src-1", Name: "Test", URL: "https://example.com",
		SourceType: "web", FetchInterval: 3600000, Enabled: true,
	}
	st.InsertSource(ctx, src)

	rep := NewRepairer(nil)
	action := rep.TryRepair(ctx, st, src, 503, fmt.Errorf("http 503"))

	if action != ActionBackoff {
		t.Fatalf("action: got %s, want backoff", action)
	}

	got, _ := st.GetSource(ctx, "src-1")
	if got.FetchInterval != 7200000 {
		t.Errorf("fetch_interval: got %d, want 7200000 (doubled)", got.FetchInterval)
	}
	if got.OriginalFetchInterval == nil || *got.OriginalFetchInterval != 3600000 {
		t.Errorf("original_fetch_interval: should be 3600000")
	}
}

func TestTryRepair_BackoffCap(t *testing.T) {
	// WHAT: Backoff caps at 24h.
	// WHY: Never wait more than a day between probes.
	db := openTestDB(t)
	st := store.NewStore(db)
	ctx := context.Background()

	src := &store.Source{
		ID: "src-2", Name: "Test", URL: "https://example.com",
		SourceType: "web", FetchInterval: 50000000, Enabled: true, // already > 12h
	}
	st.InsertSource(ctx, src)

	rep := NewRepairer(nil)
	rep.TryRepair(ctx, st, src, 500, fmt.Errorf("http 500"))

	got, _ := st.GetSource(ctx, "src-2")
	if got.FetchInterval != MaxBackoffMs {
		t.Errorf("fetch_interval: got %d, want %d (capped at max)", got.FetchInterval, MaxBackoffMs)
	}
}

func TestTryRepair_MarkBroken(t *testing.T) {
	// WHAT: 404 → mark broken.
	// WHY: Gone resources need human/LLM intervention.
	db := openTestDB(t)
	st := store.NewStore(db)
	ctx := context.Background()

	src := &store.Source{
		ID: "src-3", Name: "Gone", URL: "https://gone.com",
		SourceType: "web", Enabled: true,
	}
	st.InsertSource(ctx, src)

	rep := NewRepairer(nil)
	action := rep.TryRepair(ctx, st, src, 404, fmt.Errorf("http 404"))

	if action != ActionMarkBroken {
		t.Fatalf("action: got %s, want mark_broken", action)
	}

	got, _ := st.GetSource(ctx, "src-3")
	if got.LastStatus != "broken" {
		t.Errorf("status: got %q, want broken", got.LastStatus)
	}
}

func TestTryRepair_RotateUA(t *testing.T) {
	// WHAT: 403 on web → rotate user-agent.
	// WHY: Bot-blocking sites often accept browser UAs.
	db := openTestDB(t)
	st := store.NewStore(db)
	ctx := context.Background()

	src := &store.Source{
		ID: "src-4", Name: "Blocked", URL: "https://blocked.com",
		SourceType: "web", Enabled: true, ConfigJSON: "{}",
	}
	st.InsertSource(ctx, src)

	rep := NewRepairer(nil)
	action := rep.TryRepair(ctx, st, src, 403, fmt.Errorf("http 403"))

	if action != ActionRotateUA {
		t.Fatalf("action: got %s, want rotate_ua", action)
	}

	got, _ := st.GetSource(ctx, "src-4")
	if got.ConfigJSON == "{}" {
		t.Error("config_json should contain user_agent")
	}
}

func TestTryRepair_RotateUA_Exhausted(t *testing.T) {
	// WHAT: When all UAs exhausted → mark broken.
	// WHY: No more automatic options, needs intervention.
	db := openTestDB(t)
	st := store.NewStore(db)
	ctx := context.Background()

	// Pre-fill config with all alternate UAs as tried.
	configJSON := `{"user_agent":"ua0","tried_uas":["` + alternateUserAgents[0] + `","` + alternateUserAgents[1] + `","` + alternateUserAgents[2] + `"]}`
	src := &store.Source{
		ID: "src-5", Name: "AllTried", URL: "https://tough.com",
		SourceType: "web", Enabled: true, ConfigJSON: configJSON,
	}
	st.InsertSource(ctx, src)

	rep := NewRepairer(nil)
	action := rep.TryRepair(ctx, st, src, 403, fmt.Errorf("http 403"))

	if action != ActionMarkBroken {
		t.Fatalf("action: got %s, want mark_broken (all UAs exhausted)", action)
	}

	got, _ := st.GetSource(ctx, "src-5")
	if got.LastStatus != "broken" {
		t.Errorf("status: got %q, want broken", got.LastStatus)
	}
}

func TestTryRepair_NoAction(t *testing.T) {
	// WHAT: Unknown error → no action taken.
	// WHY: fail_count increment is the only needed response.
	db := openTestDB(t)
	st := store.NewStore(db)
	ctx := context.Background()

	src := &store.Source{
		ID: "src-6", Name: "Unknown", URL: "https://unknown.com",
		SourceType: "web", Enabled: true,
	}
	st.InsertSource(ctx, src)

	rep := NewRepairer(nil)
	action := rep.TryRepair(ctx, st, src, 0, fmt.Errorf("something weird"))

	if action != ActionNone {
		t.Fatalf("action: got %s, want none", action)
	}
}

func TestPickAlternateUA(t *testing.T) {
	// WHAT: pickAlternateUA returns UAs not yet tried.
	// WHY: Rotation must progress through all options.
	ua := pickAlternateUA("{}")
	if ua == "" {
		t.Fatal("should return a UA for empty config")
	}

	// With first UA used.
	ua2 := pickAlternateUA(`{"user_agent":"` + alternateUserAgents[0] + `"}`)
	if ua2 == alternateUserAgents[0] {
		t.Error("should not return the same UA")
	}
	if ua2 == "" {
		t.Error("should return an alternate UA")
	}
}
