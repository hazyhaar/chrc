package e2e

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hazyhaar/chrc/veille"
	"github.com/hazyhaar/pkg/connectivity"

	_ "modernc.org/sqlite"
)

func TestE2E_ConnectivityBridge(t *testing.T) {
	// WHAT: External service via ConnectivityBridge → add source → fetch → extractions.
	// WHY: Validates plug-and-play external service integration end-to-end.

	// Create a connectivity router with a mock "test_fetch" service.
	router := connectivity.New()
	callCount := 0
	router.RegisterLocal("test_fetch", func(_ context.Context, payload []byte) ([]byte, error) {
		callCount++
		resp := map[string]any{
			"extractions": []map[string]any{
				{"title": "External Post 1", "content": "Content from external service about technology.", "url": "https://ext.com/1", "content_hash": "ext-hash-1"},
				{"title": "External Post 2", "content": "Another piece of content from the bridge.", "url": "https://ext.com/2", "content_hash": "ext-hash-2"},
			},
		}
		return json.Marshal(resp)
	})

	// Create service with router.
	pool := newTestPool()
	defer pool.Close()
	cfg := &veille.Config{}
	svc, err := veille.New(pool, cfg, nil, veille.WithRouter(router))
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	ctx := context.Background()
	dossierID := "dossier-cb"

	// Add a source with the "test" type (handled by ConnectivityBridge).
	src := &veille.Source{
		Name:       "External Test Feed",
		URL:        "https://ext.com/feed",
		SourceType: "test",
		Enabled:    true,
	}
	if err := svc.AddSource(ctx, dossierID, src); err != nil {
		t.Fatalf("add source: %v", err)
	}

	// Fetch should go through ConnectivityBridge.
	if err := svc.FetchNow(ctx, dossierID, src.ID); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if callCount != 1 {
		t.Errorf("bridge call count: got %d, want 1", callCount)
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

	// Second fetch — dedup.
	svc.FetchNow(ctx, dossierID, src.ID)
	exts2, _ := svc.ListExtractions(ctx, dossierID, src.ID, 10)
	if len(exts2) != 2 {
		t.Errorf("extractions after dedup: got %d, want 2", len(exts2))
	}
}
