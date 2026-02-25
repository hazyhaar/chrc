package buffer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWrite_CreatesFile(t *testing.T) {
	// WHAT: Write creates a .md file in the pending directory.
	// WHY: Core functionality — buffer writer must produce files.
	dir := t.TempDir()
	pending := filepath.Join(dir, "pending")
	w := NewWriter(pending)

	meta := Metadata{
		ID:          "test-001",
		SourceID:    "src-1",
		DossierID:   "user-A_tech",
		SourceURL:   "https://example.com/article",
		SourceType:  "rss",
		Title:       "Test Article",
		ContentHash: "abc123",
		ExtractedAt: time.Date(2026, 2, 24, 14, 30, 0, 0, time.UTC),
	}

	path, err := w.Write(context.Background(), meta, "Hello world")
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	if filepath.Base(path) != "test-001.md" {
		t.Errorf("filename: got %q", filepath.Base(path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Hello world") {
		t.Error("body not found in file")
	}
}

func TestWrite_FrontmatterParseable(t *testing.T) {
	// WHAT: Written file has valid YAML frontmatter between --- markers.
	// WHY: Consumers must parse frontmatter to route content.
	dir := t.TempDir()
	w := NewWriter(dir)

	meta := Metadata{
		ID:          "fm-001",
		SourceID:    "src-2",
		DossierID:   "user-B_legal",
		SourceURL:   "https://example.com",
		SourceType:  "web",
		Title:       "Article: with special chars",
		ContentHash: "def456",
		ExtractedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	path, err := w.Write(context.Background(), meta, "Body text")
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	content := string(data)
	// Must start with ---
	if !strings.HasPrefix(content, "---\n") {
		t.Error("must start with ---")
	}

	// Must have closing ---
	parts := strings.SplitN(content, "---\n", 3)
	if len(parts) < 3 {
		t.Fatalf("expected 3 parts split by ---, got %d", len(parts))
	}

	fm := parts[1]
	checks := []string{
		"id: fm-001",
		"source_id: src-2",
		"dossier_id: user-B_legal",
		"source_url: https://example.com",
		"source_type: web",
		"content_hash: def456",
		"extracted_at: 2026-01-15T10:00:00Z",
	}
	for _, check := range checks {
		if !strings.Contains(fm, check) {
			t.Errorf("frontmatter missing %q", check)
		}
	}

	// Title with colon should be quoted.
	if !strings.Contains(fm, `title: "Article`) {
		t.Errorf("title with colon should be quoted, got: %s", fm)
	}

	// Body should be after the frontmatter.
	body := parts[2]
	if !strings.Contains(body, "Body text") {
		t.Error("body text not found after frontmatter")
	}
}

func TestWrite_AtomicRename(t *testing.T) {
	// WHAT: No .tmp files left after successful write.
	// WHY: Atomic write prevents consumers from reading partial files.
	dir := t.TempDir()
	w := NewWriter(dir)

	meta := Metadata{ID: "atomic-001", ExtractedAt: time.Now()}
	_, err := w.Write(context.Background(), meta, "content")
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("tmp file left behind: %s", e.Name())
		}
	}
}

func TestWrite_ConcurrentSafe(t *testing.T) {
	// WHAT: Multiple concurrent writes don't corrupt each other.
	// WHY: Pipeline may process multiple sources in parallel.
	dir := t.TempDir()
	w := NewWriter(dir)

	var wg sync.WaitGroup
	const n = 20
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			meta := Metadata{
				ID:          fmt.Sprintf("conc-%03d", idx),
				ExtractedAt: time.Now(),
			}
			_, errs[idx] = w.Write(context.Background(), meta, fmt.Sprintf("content %d", idx))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("write %d: %v", i, err)
		}
	}

	entries, _ := os.ReadDir(dir)
	mdCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			mdCount++
		}
	}
	if mdCount != n {
		t.Errorf("files: got %d, want %d", mdCount, n)
	}
}

func TestWrite_CreatesDirIfMissing(t *testing.T) {
	// WHAT: Writer creates the pending directory if it doesn't exist.
	// WHY: First-run setup should be automatic.
	dir := filepath.Join(t.TempDir(), "deep", "nested", "pending")
	w := NewWriter(dir)

	meta := Metadata{ID: "mkdir-001", ExtractedAt: time.Now()}
	path, err := w.Write(context.Background(), meta, "content")
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist: %v", err)
	}
}

func TestWrite_GeneratesIDIfEmpty(t *testing.T) {
	// WHAT: If Metadata.ID is empty, a UUID v7 is generated.
	// WHY: Caller may not always have an ID ready.
	dir := t.TempDir()
	w := NewWriter(dir)

	meta := Metadata{ExtractedAt: time.Now()}
	path, err := w.Write(context.Background(), meta, "auto-id content")
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	base := filepath.Base(path)
	if base == ".md" || base == "" {
		t.Errorf("should have generated a filename, got %q", base)
	}
}

// Ensure fmt is used.
var _ = fmt.Sprintf
