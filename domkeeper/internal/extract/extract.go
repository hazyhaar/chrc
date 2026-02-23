// Package extract implements the content extraction pipeline.
//
// It supports multiple extraction modes:
//   - css:     Extract content matching CSS selectors
//   - xpath:   Extract content matching XPath expressions
//   - density: Extract content based on text-to-markup density analysis
//   - auto:    Try CSS/XPath selectors first, fall back to density
//
// The pipeline: raw HTML → parse → select regions → clean → extract text.
package extract

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Result is the output of content extraction.
type Result struct {
	Text  string // clean extracted text
	HTML  string // extracted HTML (cleaned)
	Title string // page title if found
	Hash  string // SHA-256 of extracted text
}

// Options controls extraction behaviour.
type Options struct {
	Selectors   []string // CSS selectors or XPath expressions
	Mode        string   // "css", "xpath", "density", "auto"
	MinTextLen  int      // minimum text length to accept (default: 50)
	TrustLevel  string   // propagated to result
}

func (o *Options) defaults() {
	if o.Mode == "" {
		o.Mode = "auto"
	}
	if o.MinTextLen <= 0 {
		o.MinTextLen = 50
	}
}

// Extract runs the extraction pipeline on raw HTML.
func Extract(rawHTML []byte, opts Options) (*Result, error) {
	opts.defaults()

	doc, err := html.Parse(bytes.NewReader(rawHTML))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	title := findTitle(doc)

	switch opts.Mode {
	case "css":
		return extractCSS(doc, opts.Selectors, title, opts.MinTextLen)
	case "xpath":
		return extractXPath(doc, opts.Selectors, title, opts.MinTextLen)
	case "density":
		return extractDensity(doc, title, opts.MinTextLen)
	case "auto":
		// Try selectors first (if any), fall back to density.
		if len(opts.Selectors) > 0 {
			res, err := extractCSS(doc, opts.Selectors, title, opts.MinTextLen)
			if err == nil && len(res.Text) >= opts.MinTextLen {
				return res, nil
			}
		}
		return extractDensity(doc, title, opts.MinTextLen)
	default:
		return nil, fmt.Errorf("unknown extract mode: %q", opts.Mode)
	}
}

// findTitle extracts the page <title> text.
func findTitle(doc *html.Node) string {
	var title string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Title {
			if n.FirstChild != nil {
				title = strings.TrimSpace(n.FirstChild.Data)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return title
}

// hashText returns the SHA-256 hex digest of text.
func hashText(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
}

// renderNode serialises an HTML node subtree back to a string.
func renderNode(n *html.Node) string {
	var buf bytes.Buffer
	html.Render(&buf, n)
	return buf.String()
}

// collectText extracts all visible text from a node subtree.
func collectText(n *html.Node) string {
	var sb strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if sb.Len() > 0 {
					sb.WriteByte(' ')
				}
				sb.WriteString(text)
			}
		}
		// Skip script, style, noscript.
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Noscript:
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return sb.String()
}

// isContentTag returns true for tags likely to contain main content.
func isContentTag(a atom.Atom) bool {
	switch a {
	case atom.Main, atom.Article, atom.Section, atom.Div, atom.P,
		atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
		atom.Blockquote, atom.Pre, atom.Ul, atom.Ol, atom.Li,
		atom.Table, atom.Td, atom.Th, atom.Dl, atom.Dd, atom.Dt,
		atom.Figure, atom.Figcaption, atom.Details, atom.Summary:
		return true
	}
	return false
}

// isBoilerplate checks if a node is likely boilerplate (nav, footer, etc).
func isBoilerplate(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.DataAtom {
	case atom.Nav, atom.Footer, atom.Header, atom.Aside:
		return true
	}
	// Check common boilerplate class/id patterns.
	for _, attr := range n.Attr {
		if attr.Key == "class" || attr.Key == "id" {
			lower := strings.ToLower(attr.Val)
			for _, pattern := range boilerplatePatterns {
				if strings.Contains(lower, pattern) {
					return true
				}
			}
		}
		if attr.Key == "role" {
			switch attr.Val {
			case "navigation", "banner", "contentinfo", "complementary":
				return true
			}
		}
	}
	return false
}

var boilerplatePatterns = []string{
	"sidebar", "footer", "header", "nav", "menu", "breadcrumb",
	"cookie", "banner", "advert", "social", "share", "comment",
	"related", "widget", "popup", "modal",
}
