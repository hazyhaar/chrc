package docpipe

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	pipe := New(Config{})

	tests := []struct {
		path   string
		format Format
	}{
		{"doc.docx", FormatDocx},
		{"doc.odt", FormatODT},
		{"doc.pdf", FormatPDF},
		{"doc.md", FormatMD},
		{"doc.txt", FormatTXT},
		{"doc.html", FormatHTML},
		{"doc.htm", FormatHTML},
		{"doc.markdown", FormatMD},
	}

	for _, tt := range tests {
		f, err := pipe.Detect(tt.path)
		if err != nil {
			t.Errorf("Detect(%q): %v", tt.path, err)
			continue
		}
		if f != tt.format {
			t.Errorf("Detect(%q) = %q, want %q", tt.path, f, tt.format)
		}
	}

	// Unsupported format.
	if _, err := pipe.Detect("file.xyz"); err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestExtractText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("Hello  world\n\n  test  "), 0644)

	pipe := New(Config{})
	doc, err := pipe.Extract(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Format != FormatTXT {
		t.Fatalf("expected txt format, got %s", doc.Format)
	}
	if !strings.Contains(doc.RawText, "Hello") {
		t.Fatalf("expected text to contain Hello, got %q", doc.RawText)
	}
}

func TestExtractMarkdown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	content := `# My Title

This is a paragraph.

## Section Two

Another paragraph here.
`
	os.WriteFile(path, []byte(content), 0644)

	pipe := New(Config{})
	doc, err := pipe.Extract(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "My Title" {
		t.Fatalf("expected title 'My Title', got %q", doc.Title)
	}
	if doc.Format != FormatMD {
		t.Fatalf("expected md format, got %s", doc.Format)
	}

	// Should have headings and paragraphs.
	headings := 0
	paragraphs := 0
	for _, s := range doc.Sections {
		switch s.Type {
		case "heading":
			headings++
		case "paragraph":
			paragraphs++
		}
	}
	if headings < 2 {
		t.Fatalf("expected at least 2 headings, got %d", headings)
	}
	if paragraphs < 2 {
		t.Fatalf("expected at least 2 paragraphs, got %d", paragraphs)
	}
}

func TestExtractDocx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.docx")

	// Create a minimal .docx file.
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)

	docXML := `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Test Title</w:t></w:r></w:p>
<w:p><w:r><w:t>This is body text.</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Section Two</w:t></w:r></w:p>
<w:p><w:r><w:t>More content here.</w:t></w:r></w:p>
</w:body>
</w:document>`

	fw, _ := w.Create("word/document.xml")
	fw.Write([]byte(docXML))
	w.Close()
	f.Close()

	pipe := New(Config{})
	doc, err := pipe.Extract(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	if doc.Title != "Test Title" {
		t.Fatalf("expected title 'Test Title', got %q", doc.Title)
	}
	if len(doc.Sections) < 4 {
		t.Fatalf("expected at least 4 sections, got %d", len(doc.Sections))
	}
}

func TestExtractODT(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.odt")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)

	contentXML := `<?xml version="1.0" encoding="UTF-8"?>
<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"
  xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0">
<office:body>
<office:text>
<text:h text:outline-level="1">ODT Title</text:h>
<text:p>First paragraph.</text:p>
<text:h text:outline-level="2">Sub Heading</text:h>
<text:p>Second paragraph.</text:p>
</office:text>
</office:body>
</office:document-content>`

	fw, _ := w.Create("content.xml")
	fw.Write([]byte(contentXML))
	w.Close()
	f.Close()

	pipe := New(Config{})
	doc, err := pipe.Extract(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	if doc.Title != "ODT Title" {
		t.Fatalf("expected title 'ODT Title', got %q", doc.Title)
	}
	if len(doc.Sections) < 4 {
		t.Fatalf("expected at least 4 sections, got %d", len(doc.Sections))
	}
}

func TestExtractHTML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.html")
	html := `<!DOCTYPE html>
<html><head><title>HTML Test</title></head>
<body>
<article>
<h1>Main Heading</h1>
<p>This is a substantial paragraph of text that should be extracted by the density
algorithm because it contains enough words to pass the minimum threshold for content.</p>
</article>
</body></html>`
	os.WriteFile(path, []byte(html), 0644)

	pipe := New(Config{})
	doc, err := pipe.Extract(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	if doc.Title != "HTML Test" {
		t.Fatalf("expected title 'HTML Test', got %q", doc.Title)
	}
	if !strings.Contains(doc.RawText, "substantial paragraph") {
		t.Fatalf("expected text to contain content, got %q", doc.RawText)
	}
}

func TestSupportedFormats(t *testing.T) {
	formats := SupportedFormats()
	if len(formats) != 6 {
		t.Fatalf("expected 6 formats, got %d: %v", len(formats), formats)
	}
}
