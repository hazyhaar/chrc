package extract

import (
	"strings"
	"testing"
)

var testHTML = []byte(`<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
<nav><a href="/">Home</a> <a href="/about">About</a></nav>
<main>
<article>
<h1>Important Article</h1>
<p>This is the main content of the article. It contains important information
that should be extracted by the content extraction engine. The text is long
enough to pass the minimum length threshold for extraction.</p>
<p>Second paragraph with more relevant content about the topic being discussed.</p>
</article>
</main>
<aside>
<div class="sidebar">Related links and advertisements</div>
</aside>
<footer>Copyright 2024</footer>
</body>
</html>`)

func TestExtract_Auto(t *testing.T) {
	result, err := Extract(testHTML, Options{Mode: "auto"})
	if err != nil {
		t.Fatalf("extract auto: %v", err)
	}
	if result.Title != "Test Page" {
		t.Errorf("Title: got %q, want %q", result.Title, "Test Page")
	}
	if !strings.Contains(result.Text, "Important Article") {
		t.Errorf("Text should contain article title, got: %s", result.Text[:min(len(result.Text), 200)])
	}
	if !strings.Contains(result.Text, "main content") {
		t.Errorf("Text should contain main content, got: %s", result.Text[:min(len(result.Text), 200)])
	}
	if result.Hash == "" {
		t.Error("Hash should not be empty")
	}
}

func TestExtract_CSS(t *testing.T) {
	result, err := Extract(testHTML, Options{
		Mode:      "css",
		Selectors: []string{"article"},
	})
	if err != nil {
		t.Fatalf("extract css: %v", err)
	}
	if !strings.Contains(result.Text, "Important Article") {
		t.Errorf("CSS extraction should find article content")
	}
	// Should NOT contain nav or footer.
	if strings.Contains(result.Text, "Copyright") {
		t.Error("CSS extraction should not include footer")
	}
}

func TestExtract_Density(t *testing.T) {
	result, err := Extract(testHTML, Options{Mode: "density"})
	if err != nil {
		t.Fatalf("extract density: %v", err)
	}
	if !strings.Contains(result.Text, "main content") {
		t.Errorf("Density extraction should find main content")
	}
}

func TestExtract_XPath(t *testing.T) {
	result, err := Extract(testHTML, Options{
		Mode:      "xpath",
		Selectors: []string{"//article"},
	})
	if err != nil {
		t.Fatalf("extract xpath: %v", err)
	}
	if !strings.Contains(result.Text, "Important Article") {
		t.Errorf("XPath extraction should find article content")
	}
}

func TestExtract_CSSClassSelector(t *testing.T) {
	html := []byte(`<html><body>
<div class="content main-text">
<p>This is the actual content that needs to be extracted from the page. It has enough text to meet the threshold.</p>
</div>
<div class="sidebar">sidebar stuff</div>
</body></html>`)

	result, err := Extract(html, Options{
		Mode:      "css",
		Selectors: []string{"div.content"},
	})
	if err != nil {
		t.Fatalf("extract css class: %v", err)
	}
	if !strings.Contains(result.Text, "actual content") {
		t.Errorf("CSS class selector should match, got: %s", result.Text)
	}
}

func TestExtract_XPathAttribute(t *testing.T) {
	html := []byte(`<html><body>
<div role="main">
<p>Main content area with enough text to pass the minimum threshold for extraction purposes.</p>
</div>
<div role="complementary">sidebar</div>
</body></html>`)

	result, err := Extract(html, Options{
		Mode:      "xpath",
		Selectors: []string{"//div[@role='main']"},
	})
	if err != nil {
		t.Fatalf("extract xpath attr: %v", err)
	}
	if !strings.Contains(result.Text, "Main content area") {
		t.Errorf("XPath attr selector should match, got: %s", result.Text)
	}
}

func TestCleanText(t *testing.T) {
	input := "  Hello\u200b  world\u00ad   test  "
	got := CleanText(input)
	want := "Hello world test"
	if got != want {
		t.Errorf("CleanText: got %q, want %q", got, want)
	}
}

func TestNormaliseForHash(t *testing.T) {
	a := NormaliseForHash("Hello, World! 123")
	b := NormaliseForHash("hello world 123")
	if a != b {
		t.Errorf("NormaliseForHash: %q != %q", a, b)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
