package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/store"
)

func TestDoc_ExtractsTxt(t *testing.T) {
	// WHAT: Document handler extracts a .txt file and creates extraction.
	// WHY: Local document ingestion is a key use case.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	// Create a temp .txt file.
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "test.txt")
	os.WriteFile(txtPath, []byte("This is a test document with enough content to be extracted by the pipeline."), 0o644)

	s.InsertSource(ctx, &store.Source{
		ID: "src-doc", Name: "Doc Test", URL: txtPath,
		SourceType: "document", Enabled: true,
	})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	err := p.HandleJob(ctx, s, &Job{DossierID: "u_sp", SourceID: "src-doc", URL: txtPath})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	exts, _ := s.ListExtractions(ctx, "src-doc", 10)
	if len(exts) != 1 {
		t.Fatalf("extractions: got %d, want 1", len(exts))
	}
	if exts[0].ExtractedText == "" {
		t.Error("extracted text should not be empty")
	}
}

func TestDoc_Unchanged(t *testing.T) {
	// WHAT: Second extraction of same file is skipped (dedup).
	// WHY: Unchanged documents should not create duplicate extractions.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	dir := t.TempDir()
	txtPath := filepath.Join(dir, "stable.txt")
	os.WriteFile(txtPath, []byte("Stable content that does not change between extractions."), 0o644)

	s.InsertSource(ctx, &store.Source{
		ID: "src-stable", Name: "Stable", URL: txtPath,
		SourceType: "document", Enabled: true,
	})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)
	job := &Job{DossierID: "u_sp", SourceID: "src-stable", URL: txtPath}

	// First extraction.
	p.HandleJob(ctx, s, job)
	// Second extraction — same content.
	p.HandleJob(ctx, s, job)

	exts, _ := s.ListExtractions(ctx, "src-stable", 10)
	if len(exts) != 1 {
		t.Errorf("extractions: got %d, want 1 (dedup)", len(exts))
	}
}

func TestDoc_WritesMD(t *testing.T) {
	// WHAT: Document handler writes .md to buffer.
	// WHY: Buffer output is how documents reach the RAG island.
	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	dir := t.TempDir()
	txtPath := filepath.Join(dir, "buffered.txt")
	os.WriteFile(txtPath, []byte("Document content for buffer testing purposes with enough text."), 0o644)

	s.InsertSource(ctx, &store.Source{
		ID: "src-dbuf", Name: "DocBuf", URL: txtPath,
		SourceType: "document", Enabled: true,
	})

	bufDir := filepath.Join(t.TempDir(), "pending")
	f := fetch.New(fetch.Config{})
	p := New(f, nil)
	p.SetBuffer(buffer.NewWriter(bufDir))

	err := p.HandleJob(ctx, s, &Job{DossierID: "u_sp", SourceID: "src-dbuf", URL: txtPath})
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
	if mdCount != 1 {
		t.Errorf("buffer .md files: got %d, want 1", mdCount)
	}
}
