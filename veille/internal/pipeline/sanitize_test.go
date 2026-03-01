package pipeline

import (
	"strings"
	"testing"
)

func TestStripAllHTML_RemovesAllTags(t *testing.T) {
	// WHAT: stripAllHTML removes every HTML tag, keeping text only.
	// WHY: Strict fallback must produce clean text when converter fails.
	input := `<div class="wrapper"><span style="color:red">Hello</span> <b>world</b></div>`
	got := stripAllHTML(input)
	if strings.Contains(got, "<") {
		t.Errorf("output still contains HTML: %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "world") {
		t.Errorf("text content missing: %q", got)
	}
}

func TestStripAllHTML_Empty(t *testing.T) {
	// WHAT: Empty input returns empty.
	// WHY: Edge case.
	if got := stripAllHTML(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestStripAllHTML_PlainText(t *testing.T) {
	// WHAT: Plain text passes through unchanged.
	// WHY: Fallback on non-HTML content must not corrupt it.
	input := "Just plain text with no tags"
	got := stripAllHTML(input)
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestNewHTMLSanitizer_KeepsSemantic(t *testing.T) {
	// WHAT: Sanitizer preserves semantic HTML elements.
	// WHY: html-to-markdown needs structure to produce good output.
	p := newHTMLSanitizer()

	cases := []struct {
		name  string
		input string
		must  []string // substrings that must be present
	}{
		{
			name:  "headings",
			input: `<h1>Title</h1><h2>Subtitle</h2>`,
			must:  []string{"<h1>", "<h2>", "Title", "Subtitle"},
		},
		{
			name:  "paragraphs",
			input: `<p>Text</p><br>`,
			must:  []string{"<p>", "Text"},
		},
		{
			name:  "lists",
			input: `<ul><li>One</li><li>Two</li></ul>`,
			must:  []string{"<ul>", "<li>", "One", "Two"},
		},
		{
			name:  "tables",
			input: `<table><tr><th>Header</th></tr><tr><td>Cell</td></tr></table>`,
			must:  []string{"<table>", "<tr>", "<th>", "<td>", "Header", "Cell"},
		},
		{
			name:  "links",
			input: `<a href="https://example.com">Link</a>`,
			must:  []string{`<a href="https://example.com"`, "Link"},
		},
		{
			name:  "images",
			input: `<img src="img.png" alt="photo">`,
			must:  []string{`src="img.png"`, `alt="photo"`},
		},
		{
			name:  "formatting",
			input: `<strong>bold</strong> <em>italic</em> <code>code</code>`,
			must:  []string{"<strong>", "<em>", "<code>", "bold", "italic", "code"},
		},
		{
			name:  "blockquote_pre",
			input: `<blockquote>Quote</blockquote><pre>Code</pre>`,
			must:  []string{"<blockquote>", "<pre>", "Quote", "Code"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Sanitize(tc.input)
			for _, m := range tc.must {
				if !strings.Contains(got, m) {
					t.Errorf("missing %q in output: %q", m, got)
				}
			}
		})
	}
}

func TestNewHTMLSanitizer_StripsNoise(t *testing.T) {
	// WHAT: Sanitizer removes decorative/layout HTML and attributes.
	// WHY: CSS inline, class, id, style, script, nav, footer corrupt markdown output.
	p := newHTMLSanitizer()

	cases := []struct {
		name   string
		input  string
		absent []string // substrings that must NOT be present
	}{
		{
			name:   "style_attribute",
			input:  `<p style="color:red;font-size:14px">Text</p>`,
			absent: []string{"style=", "color:red"},
		},
		{
			name:   "class_attribute",
			input:  `<div class="wrapper"><p class="intro">Text</p></div>`,
			absent: []string{"class=", "wrapper", "<div"},
		},
		{
			name:   "span_tags",
			input:  `<span style="color:#666">Decorative</span>`,
			absent: []string{"<span", "style="},
		},
		{
			name:   "script_tag",
			input:  `<p>Before</p><script>alert(1)</script><p>After</p>`,
			absent: []string{"<script", "alert"},
		},
		{
			name:   "style_tag",
			input:  `<style>.x{color:red}</style><p>Text</p>`,
			absent: []string{"<style", ".x{color"},
		},
		{
			name:   "nav_footer",
			input:  `<nav><a href="/">Home</a></nav><footer>Copyright</footer>`,
			absent: []string{"<nav", "<footer"},
		},
		{
			name:   "div_wrapper",
			input:  `<div id="main"><p>Content</p></div>`,
			absent: []string{"<div", "id="},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Sanitize(tc.input)
			for _, a := range tc.absent {
				if strings.Contains(got, a) {
					t.Errorf("should not contain %q in output: %q", a, got)
				}
			}
		})
	}
}

func TestNewHTMLSanitizer_RSSNewsletter(t *testing.T) {
	// WHAT: Real-world RSS newsletter with nested tables + inline CSS is cleaned.
	// WHY: This is the exact pattern that caused the 12.6% pollution in shards.
	p := newHTMLSanitizer()

	input := `<table style="width:100%;background:#f5f5f5">
<tr><td style="padding:20px">
<table style="max-width:600px;margin:0 auto">
<tr><td>
<h2 style="color:#333">Go Weekly #512</h2>
<p style="font-size:14px">Welcome to this week's issue.</p>
<table style="width:100%">
<tr><td style="border-bottom:1px solid #ddd;padding:10px">
<a href="https://example.com/article1" style="color:#1a73e8;text-decoration:none">
<strong>New in Go 1.25</strong></a>
<br><span style="color:#666">A look at the upcoming release features.</span>
</td></tr>
<tr><td style="padding:10px">
<a href="https://example.com/article2"><strong>Error Handling Patterns</strong></a>
<br><span>Best practices for Go error handling.</span>
</td></tr>
</table>
</td></tr></table>
</td></tr></table>`

	got := p.Sanitize(input)

	// Text content must survive
	for _, must := range []string{
		"Go Weekly #512",
		"Welcome to this week",
		"New in Go 1.25",
		"A look at the upcoming release features.",
		"Error Handling Patterns",
		"Best practices for Go error handling.",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("missing text %q in sanitized output:\n%s", must, got)
		}
	}

	// Links preserved
	if !strings.Contains(got, `href="https://example.com/article1"`) {
		t.Errorf("link href should survive sanitization:\n%s", got)
	}

	// Structure preserved
	if !strings.Contains(got, "<h2>") {
		t.Errorf("h2 should survive sanitization:\n%s", got)
	}
	if !strings.Contains(got, "<table>") {
		t.Errorf("table should survive sanitization:\n%s", got)
	}

	// Noise removed
	for _, absent := range []string{"style=", "color:#", "padding:", "<span", "<div", "class="} {
		if strings.Contains(got, absent) {
			t.Errorf("noise %q should be removed:\n%s", absent, got)
		}
	}
}

func TestNewHTMLSanitizer_SyntaxHighlightingSpans(t *testing.T) {
	// WHAT: Code blocks with syntax highlighting spans (common in RSS) are cleaned.
	// WHY: Prism/highlight.js wraps tokens in spans with classes.
	p := newHTMLSanitizer()

	input := `<pre><code><span class="token keyword">func</span> <span class="token function">main</span><span class="token punctuation">(</span><span class="token punctuation">)</span> <span class="token punctuation">{</span>
    fmt<span class="token punctuation">.</span><span class="token function">Println</span><span class="token punctuation">(</span><span class="token string">"hello"</span><span class="token punctuation">)</span>
<span class="token punctuation">}</span></code></pre>`

	got := p.Sanitize(input)

	// pre/code must survive
	if !strings.Contains(got, "<pre>") || !strings.Contains(got, "<code>") {
		t.Errorf("pre/code tags should survive:\n%s", got)
	}

	// Text content preserved
	if !strings.Contains(got, "func") || !strings.Contains(got, "main") || !strings.Contains(got, "Println") {
		t.Errorf("code text should survive:\n%s", got)
	}

	// Span noise removed
	if strings.Contains(got, "<span") || strings.Contains(got, "class=") {
		t.Errorf("span/class should be stripped:\n%s", got)
	}
}

func TestHtmlToMarkdown_PreCleansHTML(t *testing.T) {
	// WHAT: htmlToMarkdown pre-cleans with bluemonday before converting.
	// WHY: Converter fails on CSS-heavy HTML; pre-clean fixes that.
	p := New(nil, nil)

	input := `<div style="background:#eee"><h1 style="color:blue">Title</h1><p style="font-size:14px">Paragraph with <span style="color:red">colored</span> text.</p></div>`

	got := p.htmlToMarkdown(input, "https://example.com", "fallback")

	// Should contain converted markdown
	if !strings.Contains(got, "Title") {
		t.Errorf("title missing: %q", got)
	}
	if !strings.Contains(got, "Paragraph") {
		t.Errorf("paragraph missing: %q", got)
	}

	// Should NOT contain HTML tags
	if strings.Contains(got, "<div") || strings.Contains(got, "<span") || strings.Contains(got, "style=") {
		t.Errorf("HTML artifacts in markdown output: %q", got)
	}
}

func TestHtmlToMarkdown_EmptyUsesStrippedFallback(t *testing.T) {
	// WHAT: Empty HTML returns stripped fallback.
	// WHY: Fallback itself may contain HTML from RSS content:encoded.
	p := New(nil, nil)

	got := p.htmlToMarkdown("", "https://example.com", `<b>Bold</b> text <span style="color:red">colored</span>`)

	if strings.Contains(got, "<") {
		t.Errorf("fallback should be stripped of HTML: %q", got)
	}
	if !strings.Contains(got, "Bold") || !strings.Contains(got, "text") {
		t.Errorf("text content should survive stripping: %q", got)
	}
}

func TestHtmlToMarkdown_FallbackOnConverterFailure(t *testing.T) {
	// WHAT: When converter produces empty, fallback is stripped.
	// WHY: Some HTML is so broken that even after sanitization the converter fails.
	p := New(nil, nil)

	// Completely empty after sanitization (only script/style tags).
	got := p.htmlToMarkdown("<script>x</script><style>.a{}</style>", "https://example.com", "plain fallback")

	if got != "plain fallback" {
		t.Errorf("expected plain fallback, got %q", got)
	}
}
