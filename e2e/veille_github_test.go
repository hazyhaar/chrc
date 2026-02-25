package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hazyhaar/chrc/veille"
	"github.com/hazyhaar/pkg/connectivity"

	_ "modernc.org/sqlite"
)

func TestE2E_GitHubViaConnectivity(t *testing.T) {
	// WHAT: Full service: add github source → fetch via connectivity → verify extractions.
	// WHY: Verifies the complete wiring (router → service → pipeline → store).

	apiResponse := `[
		{
			"sha": "e2e-sha-1",
			"html_url": "https://github.com/test/repo/commit/e2e-sha-1",
			"commit": {"message": "feat: first commit for end-to-end test verification."}
		},
		{
			"sha": "e2e-sha-2",
			"html_url": "https://github.com/test/repo/commit/e2e-sha-2",
			"commit": {"message": "fix: second commit fixing a critical bug in the system."}
		}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(apiResponse))
	}))
	defer srv.Close()

	// Create connectivity router with GitHub service.
	router := connectivity.New()
	router.RegisterLocal("github_fetch", veille.NewGitHubService(srv.URL))

	// Create service with router — DiscoverHandlers will find github_fetch.
	pool := newTestPool()
	defer pool.Close()
	cfg := &veille.Config{}
	svc, err := veille.New(pool, cfg, nil, veille.WithRouter(router))
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	ctx := context.Background()
	dossierID := "dossier-gh"

	// Add a GitHub source.
	src := &veille.Source{
		Name:       "GitHub Test Repo",
		URL:        "https://github.com/test/repo",
		SourceType: "github",
		Enabled:    true,
	}
	if err := svc.AddSource(ctx, dossierID, src); err != nil {
		t.Fatalf("add source: %v", err)
	}

	// Fetch should go through ConnectivityBridge → github_fetch service.
	if err := svc.FetchNow(ctx, dossierID, src.ID); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	// Verify extractions were stored.
	exts, _ := svc.ListExtractions(ctx, dossierID, src.ID, 10)
	if len(exts) != 2 {
		t.Fatalf("extractions: got %d, want 2", len(exts))
	}

	// Verify fetch log.
	history, _ := svc.FetchHistory(ctx, dossierID, src.ID, 10)
	if len(history) != 1 {
		t.Fatalf("fetch history: got %d, want 1", len(history))
	}
	if history[0].Status != "ok" {
		t.Errorf("fetch status: got %q", history[0].Status)
	}

	// Second fetch — dedup (same shas).
	svc.FetchNow(ctx, dossierID, src.ID)
	exts2, _ := svc.ListExtractions(ctx, dossierID, src.ID, 10)
	if len(exts2) != 2 {
		t.Errorf("extractions after dedup: got %d, want 2", len(exts2))
	}

	// Search — should find commits by content.
	results, _ := svc.Search(ctx, dossierID, "critical bug", 10)
	if len(results) == 0 {
		t.Error("search should find results for 'critical bug'")
	}

	// Stats.
	stats, _ := svc.Stats(ctx, dossierID)
	if stats.Sources != 1 {
		t.Errorf("sources: got %d, want 1", stats.Sources)
	}
	if stats.Extractions != 2 {
		t.Errorf("extractions: got %d, want 2", stats.Extractions)
	}
}
