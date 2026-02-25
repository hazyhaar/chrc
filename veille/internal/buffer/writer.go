// CLAUDE:SUMMARY Atomic .md file writer with YAML frontmatter for RAG buffer consumption.
// Package buffer writes extracted content as .md files to a filesystem buffer
// for asynchronous consumption by a RAG island.
//
// Each file includes YAML frontmatter with source metadata, and the body
// is the cleaned extracted text. Files are written atomically (write .tmp
// then rename) to prevent partial reads by consumers.
package buffer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hazyhaar/pkg/idgen"
)

// Metadata describes the source and extraction context for a .md file.
type Metadata struct {
	ID          string
	SourceID    string
	DossierID   string
	SourceURL   string
	SourceType  string
	Title       string
	ContentHash string
	ExtractedAt time.Time
}

// Writer deposits .md files into the pending directory.
type Writer struct {
	dir   string // buffer/pending/
	newID func() string
}

// NewWriter creates a Writer targeting the given pending directory.
// The directory is created on first write if it does not exist.
func NewWriter(pendingDir string) *Writer {
	return &Writer{
		dir:   pendingDir,
		newID: idgen.New,
	}
}

// Write creates a .md file with YAML frontmatter + text body.
// Returns the path of the written file.
func (w *Writer) Write(ctx context.Context, meta Metadata, text string) (string, error) {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return "", fmt.Errorf("buffer: mkdir %s: %w", w.dir, err)
	}

	if meta.ID == "" {
		meta.ID = w.newID()
	}

	filename := meta.ID + ".md"
	target := filepath.Join(w.dir, filename)
	tmp := target + ".tmp"

	content := formatFrontmatter(meta) + text

	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("buffer: write tmp: %w", err)
	}

	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("buffer: rename: %w", err)
	}

	return target, nil
}

// formatFrontmatter builds a YAML frontmatter block.
func formatFrontmatter(m Metadata) string {
	return "---\n" +
		"id: " + m.ID + "\n" +
		"source_id: " + m.SourceID + "\n" +
		"dossier_id: " + m.DossierID + "\n" +
		"source_url: " + m.SourceURL + "\n" +
		"source_type: " + m.SourceType + "\n" +
		"title: " + yamlEscape(m.Title) + "\n" +
		"extracted_at: " + m.ExtractedAt.UTC().Format(time.RFC3339) + "\n" +
		"content_hash: " + m.ContentHash + "\n" +
		"---\n\n"
}

// yamlEscape wraps a string in quotes if it contains special YAML characters.
func yamlEscape(s string) string {
	for _, c := range s {
		if c == ':' || c == '#' || c == '\'' || c == '"' || c == '{' || c == '}' || c == '[' || c == ']' || c == ',' || c == '&' || c == '*' || c == '?' || c == '|' || c == '-' || c == '<' || c == '>' || c == '=' || c == '!' || c == '%' || c == '@' || c == '`' || c == '\n' {
			return `"` + escapeDoubleQuotes(s) + `"`
		}
	}
	return s
}

func escapeDoubleQuotes(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			result = append(result, '\\', '"')
		} else if s[i] == '\\' {
			result = append(result, '\\', '\\')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}
