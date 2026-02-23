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

func TestRuleCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Insert.
	r := &Rule{
		ID:          "rule-1",
		Name:        "Test Rule",
		URLPattern:  "https://example.com/*",
		Selectors:   []string{"article", "main"},
		ExtractMode: "css",
		TrustLevel:  "community",
		Enabled:     true,
		Priority:    10,
	}
	if err := s.InsertRule(ctx, r); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Get.
	got, err := s.GetRule(ctx, "rule-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("get: got nil")
	}
	if got.Name != "Test Rule" {
		t.Errorf("Name: got %q, want %q", got.Name, "Test Rule")
	}
	if got.ExtractMode != "css" {
		t.Errorf("ExtractMode: got %q, want %q", got.ExtractMode, "css")
	}
	if len(got.Selectors) != 2 {
		t.Errorf("Selectors: got %d, want 2", len(got.Selectors))
	}
	if !got.Enabled {
		t.Error("Enabled: got false, want true")
	}
	if got.Priority != 10 {
		t.Errorf("Priority: got %d, want 10", got.Priority)
	}

	// List.
	rules, err := s.ListRules(ctx, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("list: got %d rules, want 1", len(rules))
	}

	// Update.
	got.Name = "Updated Rule"
	if err := s.UpdateRule(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := s.GetRule(ctx, "rule-1")
	if got2.Name != "Updated Rule" {
		t.Errorf("Name after update: got %q, want %q", got2.Name, "Updated Rule")
	}
	if got2.Version != 2 {
		t.Errorf("Version after update: got %d, want 2", got2.Version)
	}

	// Delete.
	if err := s.DeleteRule(ctx, "rule-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got3, _ := s.GetRule(ctx, "rule-1")
	if got3 != nil {
		t.Error("get after delete: expected nil")
	}
}

func TestFolderCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	f := &Folder{
		ID:          "folder-1",
		Name:        "Research",
		Description: "Research papers",
	}
	if err := s.InsertFolder(ctx, f); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := s.GetFolder(ctx, "folder-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Research" {
		t.Errorf("Name: got %q, want %q", got.Name, "Research")
	}

	folders, err := s.ListFolders(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(folders) != 1 {
		t.Fatalf("list: got %d folders, want 1", len(folders))
	}

	if err := s.DeleteFolder(ctx, "folder-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestContentCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Need a rule first.
	r := &Rule{
		ID:          "rule-1",
		Name:        "test",
		URLPattern:  "*",
		ExtractMode: "auto",
		TrustLevel:  "unverified",
		Enabled:     true,
	}
	s.InsertRule(ctx, r)

	c := &Content{
		ID:            "content-1",
		RuleID:        "rule-1",
		PageURL:       "https://example.com",
		ContentHash:   "abc123",
		ExtractedText: "Hello world extracted content",
		Title:         "Example Page",
		TrustLevel:    "community",
	}

	// Insert.
	isNew, err := s.InsertContent(ctx, c)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !isNew {
		t.Error("insert: expected isNew=true")
	}

	// Dedup: same hash should return isNew=false.
	c2 := &Content{
		ID:            "content-2",
		RuleID:        "rule-1",
		PageURL:       "https://example.com",
		ContentHash:   "abc123",
		ExtractedText: "same content",
		TrustLevel:    "community",
	}
	isNew2, err := s.InsertContent(ctx, c2)
	if err != nil {
		t.Fatalf("insert dedup: %v", err)
	}
	if isNew2 {
		t.Error("insert dedup: expected isNew=false for same hash")
	}

	// Get.
	got, err := s.GetContent(ctx, "content-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Example Page" {
		t.Errorf("Title: got %q, want %q", got.Title, "Example Page")
	}
}

func TestChunksAndSearch(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Set up rule + content.
	s.InsertRule(ctx, &Rule{
		ID: "rule-1", Name: "test", URLPattern: "*",
		ExtractMode: "auto", TrustLevel: "official", Enabled: true,
	})
	s.InsertContent(ctx, &Content{
		ID: "content-1", RuleID: "rule-1", PageURL: "https://example.com",
		ContentHash: "h1", ExtractedText: "test content", Title: "Test",
		TrustLevel: "official",
	})

	// Insert chunks.
	chunks := []*Chunk{
		{ID: "chunk-1", ContentID: "content-1", ChunkIndex: 0, Text: "Quantum computing is a revolutionary technology", TokenCount: 7},
		{ID: "chunk-2", ContentID: "content-1", ChunkIndex: 1, Text: "Machine learning algorithms process large datasets", TokenCount: 6},
	}
	if err := s.InsertChunks(ctx, chunks); err != nil {
		t.Fatalf("insert chunks: %v", err)
	}

	// Get chunks by content.
	got, err := s.GetChunksByContent(ctx, "content-1")
	if err != nil {
		t.Fatalf("get chunks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("chunks: got %d, want 2", len(got))
	}
	if got[0].Text != "Quantum computing is a revolutionary technology" {
		t.Errorf("chunk[0]: got %q", got[0].Text)
	}

	// FTS search.
	results, err := s.Search(ctx, SearchOptions{Query: "quantum computing", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("search results: got %d, want 1", len(results))
	}
	if results[0].ContentTitle != "Test" {
		t.Errorf("search result title: got %q, want %q", results[0].ContentTitle, "Test")
	}

	// Count.
	n, err := s.CountChunks(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("chunk count: got %d, want 2", n)
	}
}

func TestMatchRules(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	s.InsertRule(ctx, &Rule{
		ID: "r1", Name: "example", URLPattern: "https://example.com/*",
		ExtractMode: "auto", TrustLevel: "official", Enabled: true, Priority: 10,
	})
	s.InsertRule(ctx, &Rule{
		ID: "r2", Name: "other", URLPattern: "https://other.com/*",
		ExtractMode: "css", TrustLevel: "community", Enabled: true, Priority: 5,
	})
	s.InsertRule(ctx, &Rule{
		ID: "r3", Name: "disabled", URLPattern: "https://example.com/*",
		ExtractMode: "auto", TrustLevel: "unverified", Enabled: false,
	})

	// Match by URL.
	rules, err := s.MatchRules(ctx, "https://example.com/page1", "")
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("match: got %d rules, want 1", len(rules))
	}
	if rules[0].ID != "r1" {
		t.Errorf("matched rule: got %q, want %q", rules[0].ID, "r1")
	}
}

func TestIngestEntry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	e := &IngestEntry{
		ID:      "ie-1",
		BatchID: "batch-1",
		PageURL: "https://example.com",
		Status:  "processing",
	}
	if err := s.InsertIngestEntry(ctx, e); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := s.CompleteIngestEntry(ctx, "ie-1", "done", "", 3); err != nil {
		t.Fatalf("complete: %v", err)
	}

	entries, err := s.RecentIngestEntries(ctx, 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries: got %d, want 1", len(entries))
	}
	if entries[0].Status != "done" {
		t.Errorf("status: got %q, want %q", entries[0].Status, "done")
	}
	if entries[0].ExtractedCount != 3 {
		t.Errorf("extracted_count: got %d, want 3", entries[0].ExtractedCount)
	}
}

func TestSourcePage(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p := &SourcePage{
		PageID:  "page-1",
		PageURL: "https://example.com",
	}
	if err := s.UpsertSourcePage(ctx, p); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetSourcePage(ctx, "page-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PageURL != "https://example.com" {
		t.Errorf("PageURL: got %q", got.PageURL)
	}

	pages, err := s.ListSourcePages(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("pages: got %d, want 1", len(pages))
	}
}
