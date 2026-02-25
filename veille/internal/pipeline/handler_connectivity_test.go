package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/store"
	"github.com/hazyhaar/pkg/connectivity"
)

func TestConnectivityBridge_CallsRouter(t *testing.T) {
	// WHAT: ConnectivityBridge calls the router service and stores extractions.
	// WHY: Bridge must relay to external services and store results.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	router := connectivity.New()
	var gotPayload []byte
	router.RegisterLocal("socmed_fetch", func(_ context.Context, payload []byte) ([]byte, error) {
		gotPayload = payload
		resp := bridgeResponse{
			Extractions: []bridgeExtraction{
				{Title: "Post 1", Content: "Social media post about technology.", URL: "https://social.com/1", ContentHash: "hash-1"},
				{Title: "Post 2", Content: "Another post about programming.", URL: "https://social.com/2", ContentHash: "hash-2"},
			},
		}
		return json.Marshal(resp)
	})

	s.InsertSource(ctx, &store.Source{
		ID: "src-socmed", Name: "Social Feed", URL: "https://social.com/feed",
		SourceType: "socmed", Enabled: true, ConfigJSON: `{"account":"test"}`,
	})
	src, _ := s.GetSource(ctx, "src-socmed")

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	bridge := NewConnectivityBridge(router, "socmed_fetch", "socmed")
	p.currentJob = &Job{DossierID: "u1_s1", SourceID: "src-socmed", URL: src.URL}

	err := bridge.Handle(ctx, s, src, p)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	// Verify payload was sent.
	if gotPayload == nil {
		t.Fatal("router was not called")
	}
	var req bridgeRequest
	json.Unmarshal(gotPayload, &req)
	if req.SourceID != "src-socmed" {
		t.Errorf("source_id: got %q", req.SourceID)
	}

	// Verify extractions stored.
	exts, _ := s.ListExtractions(ctx, "src-socmed", 10)
	if len(exts) != 2 {
		t.Fatalf("extractions: got %d, want 2", len(exts))
	}
	if exts[0].Title != "Post 2" && exts[1].Title != "Post 2" {
		t.Error("expected Post 2 in extractions")
	}
}

func TestConnectivityBridge_Dedup(t *testing.T) {
	// WHAT: Second call with same hashes produces 0 new extractions.
	// WHY: Dedup prevents duplicates from external services.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	router := connectivity.New()
	router.RegisterLocal("test_fetch", func(_ context.Context, _ []byte) ([]byte, error) {
		resp := bridgeResponse{
			Extractions: []bridgeExtraction{
				{Title: "Stable", Content: "Content that does not change between calls.", URL: "https://test.com/stable", ContentHash: "stable-hash"},
			},
		}
		return json.Marshal(resp)
	})

	s.InsertSource(ctx, &store.Source{
		ID: "src-dedup", Name: "Dedup Test", URL: "https://test.com",
		SourceType: "test", Enabled: true,
	})
	src, _ := s.GetSource(ctx, "src-dedup")

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	bridge := NewConnectivityBridge(router, "test_fetch", "test")
	p.currentJob = &Job{DossierID: "u1_s1", SourceID: "src-dedup"}

	// First call.
	bridge.Handle(ctx, s, src, p)
	exts1, _ := s.ListExtractions(ctx, "src-dedup", 10)
	if len(exts1) != 1 {
		t.Fatalf("first call: got %d extractions, want 1", len(exts1))
	}

	// Second call — same hash.
	bridge.Handle(ctx, s, src, p)
	exts2, _ := s.ListExtractions(ctx, "src-dedup", 10)
	if len(exts2) != 1 {
		t.Errorf("second call: got %d extractions, want 1 (dedup)", len(exts2))
	}
}

func TestDiscoverHandlers_RegistersFromRouter(t *testing.T) {
	// WHAT: DiscoverHandlers finds *_fetch services and registers bridge handlers.
	// WHY: Auto-discovery enables plug-and-play external services.
	router := connectivity.New()
	router.RegisterLocal("socmed_fetch", func(_ context.Context, _ []byte) ([]byte, error) {
		return []byte(`{}`), nil
	})
	router.RegisterLocal("mastodon_fetch", func(_ context.Context, _ []byte) ([]byte, error) {
		return []byte(`{}`), nil
	})
	router.RegisterLocal("unrelated_service", func(_ context.Context, _ []byte) ([]byte, error) {
		return []byte(`{}`), nil
	})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	DiscoverHandlers(p, router)

	// socmed and mastodon should be registered.
	if _, ok := p.handlers["socmed"]; !ok {
		t.Error("socmed handler not registered")
	}
	if _, ok := p.handlers["mastodon"]; !ok {
		t.Error("mastodon handler not registered")
	}
	// unrelated_service should NOT be registered (no _fetch suffix).
	if _, ok := p.handlers["unrelated_service"]; ok {
		t.Error("unrelated_service should not be registered")
	}
}

func TestDiscoverHandlers_SkipsBuiltins(t *testing.T) {
	// WHAT: DiscoverHandlers does not override built-in handlers.
	// WHY: Built-in web/rss/api handlers must not be replaced by bridge.
	router := connectivity.New()
	router.RegisterLocal("web_fetch", func(_ context.Context, _ []byte) ([]byte, error) {
		return []byte(`{}`), nil
	})
	router.RegisterLocal("rss_fetch", func(_ context.Context, _ []byte) ([]byte, error) {
		return []byte(`{}`), nil
	})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	// Verify built-ins are WebHandler/RSSHandler.
	webBefore := p.handlers["web"]
	rssBefore := p.handlers["rss"]

	DiscoverHandlers(p, router)

	// Built-ins should NOT be replaced.
	if p.handlers["web"] != webBefore {
		t.Error("web handler was replaced by bridge")
	}
	if p.handlers["rss"] != rssBefore {
		t.Error("rss handler was replaced by bridge")
	}
}
