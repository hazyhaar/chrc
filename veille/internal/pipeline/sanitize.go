// CLAUDE:SUMMARY Bluemonday HTML pre-clean for html-to-markdown: keeps semantic structure, strips CSS/scripts/decorative spans.
// CLAUDE:DEPENDS github.com/microcosm-cc/bluemonday
// CLAUDE:EXPORTS newHTMLSanitizer, stripAllHTML
package pipeline

import "github.com/microcosm-cc/bluemonday"

// newHTMLSanitizer returns a bluemonday policy that keeps semantic structure
// (needed by html-to-markdown) and strips everything else (CSS inline styles,
// decorative spans, scripts, nav, footer, class/id attributes).
func newHTMLSanitizer() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// Headings
	p.AllowElements("h1", "h2", "h3", "h4", "h5", "h6")

	// Block structure
	p.AllowElements("p", "br", "hr")

	// Lists
	p.AllowElements("ul", "ol", "li", "dl", "dt", "dd")

	// Tables
	p.AllowElements("table", "thead", "tbody", "tfoot", "tr", "th", "td", "caption")

	// Code / quotes
	p.AllowElements("pre", "code", "blockquote")

	// Inline formatting
	p.AllowElements("strong", "b", "em", "i", "u", "s", "del", "sub", "sup", "mark")

	// Links (href only)
	p.AllowElements("a")
	p.AllowAttrs("href").OnElements("a")

	// Images (src + alt only)
	p.AllowElements("img")
	p.AllowAttrs("src", "alt").OnElements("img")

	// Figures
	p.AllowElements("figure", "figcaption")

	// Details/summary
	p.AllowElements("details", "summary")

	// Everything else (div, span, nav, footer, style=, class=, script) is stripped.
	return p
}

// stripAllHTML removes all HTML tags, keeping only text content.
// Used as strict fallback when markdown conversion fails.
func stripAllHTML(html string) string {
	return bluemonday.StrictPolicy().Sanitize(html)
}
