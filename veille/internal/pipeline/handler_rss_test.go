package pipeline

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/store"
)

const testRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>https://feed.example.com</link>
    <item>
      <guid>guid-001</guid>
      <title>First Article</title>
      <link>https://feed.example.com/first</link>
      <description>First article description with enough content to be meaningful.</description>
    </item>
    <item>
      <guid>guid-002</guid>
      <title>Second Article</title>
      <link>https://feed.example.com/second</link>
      <description>Second article description also with enough content to extract.</description>
    </item>
  </channel>
</rss>`

func TestRSS_CreatesExtractions(t *testing.T) {
	// WHAT: RSS handler creates one extraction per feed entry.
	// WHY: Each feed entry is a distinct content unit.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSS))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{
		ID: "src-rss", Name: "RSS Test", URL: srv.URL,
		SourceType: "rss", Enabled: true,
	})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	err := p.HandleJob(ctx, s, &Job{DossierID: "u_sp", SourceID: "src-rss", URL: srv.URL})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	exts, _ := s.ListExtractions(ctx, "src-rss", 10)
	if len(exts) != 2 {
		t.Fatalf("extractions: got %d, want 2", len(exts))
	}

	// Verify titles.
	titles := map[string]bool{}
	for _, e := range exts {
		titles[e.Title] = true
	}
	if !titles["First Article"] {
		t.Error("missing 'First Article'")
	}
	if !titles["Second Article"] {
		t.Error("missing 'Second Article'")
	}
}

func TestRSS_Dedup(t *testing.T) {
	// WHAT: Second fetch of same feed doesn't create duplicate extractions.
	// WHY: RSS feeds contain previously-seen entries on every fetch.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(testRSS))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{
		ID: "src-rdup", Name: "Dedup", URL: srv.URL,
		SourceType: "rss", Enabled: true,
	})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	job := &Job{DossierID: "u_sp", SourceID: "src-rdup", URL: srv.URL}

	// First fetch.
	p.HandleJob(ctx, s, job)
	// Second fetch — same entries.
	p.HandleJob(ctx, s, job)

	exts, _ := s.ListExtractions(ctx, "src-rdup", 20)
	if len(exts) != 2 {
		t.Errorf("extractions: got %d, want 2 (dedup should prevent doubles)", len(exts))
	}
}

func TestRSS_FollowLinks(t *testing.T) {
	// WHAT: With follow_links=true, the handler fetches and extracts linked pages.
	// WHY: RSS descriptions are often truncated; full content lives at the link.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	pageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!DOCTYPE html><html><head><title>Full Page</title></head>
		<body><main><p>This is the full article content extracted from the linked page.
		It contains much more detail than the RSS description summary provides.</p></main></body></html>`))
	}))
	defer pageSrv.Close()

	feedXML := `<?xml version="1.0"?><rss version="2.0"><channel><title>Follow</title>
	<item><guid>fl-001</guid><title>Follow Me</title><link>` + pageSrv.URL + `</link>
	<description>Short description</description></item></channel></rss>`

	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(feedXML))
	}))
	defer feedSrv.Close()

	s.InsertSource(ctx, &store.Source{
		ID: "src-fl", Name: "Follow", URL: feedSrv.URL,
		SourceType: "rss", Enabled: true,
		ConfigJSON: `{"follow_links": true}`,
	})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	err := p.HandleJob(ctx, s, &Job{DossierID: "u_sp", SourceID: "src-fl", URL: feedSrv.URL})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	exts, _ := s.ListExtractions(ctx, "src-fl", 10)
	if len(exts) != 1 {
		t.Fatalf("extractions: got %d", len(exts))
	}

	// The extraction should have the full page content, not just "Short description".
	if len(exts[0].ExtractedText) < 50 {
		t.Errorf("text too short (follow_links should have fetched full page): %d chars", len(exts[0].ExtractedText))
	}
}

func TestRSS_WritesBuffer(t *testing.T) {
	// WHAT: RSS handler writes .md files to buffer for each entry.
	// WHY: RAG island consumes .md files from the buffer.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(testRSS))
	}))
	defer srv.Close()

	s.InsertSource(ctx, &store.Source{
		ID: "src-rbuf", Name: "RSS Buf", URL: srv.URL,
		SourceType: "rss", Enabled: true,
	})

	bufDir := filepath.Join(t.TempDir(), "pending")
	f := fetch.New(fetch.Config{})
	p := New(f, nil)
	p.SetBuffer(buffer.NewWriter(bufDir))

	err := p.HandleJob(ctx, s, &Job{DossierID: "u1_s1", SourceID: "src-rbuf", URL: srv.URL})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	entries, _ := os.ReadDir(bufDir)
	mdCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" {
			mdCount++
		}
	}
	if mdCount != 2 {
		t.Errorf("buffer .md files: got %d, want 2", mdCount)
	}
}
