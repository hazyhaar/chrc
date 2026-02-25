// CLAUDE:SUMMARY Extracts structured sections (headings, paragraphs, tables, lists) from HTML files.
package docpipe

import (
	"bytes"
	"os"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// extractHTMLFile extracts structured content from an HTML file.
func extractHTMLFile(path string) (string, []Section, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}

	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return "", nil, err
	}

	title := findHTMLTitle(doc)

	var sections []Section
	extractHTMLNodes(doc, &sections)

	if len(sections) == 0 {
		// Fallback: extract all text.
		text := collectHTMLText(doc)
		if text != "" {
			sections = append(sections, Section{Text: text, Type: "paragraph"})
		}
	}

	return title, sections, nil
}

// findHTMLTitle extracts the <title> text.
func findHTMLTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.DataAtom == atom.Title {
		if n.FirstChild != nil {
			return strings.TrimSpace(n.FirstChild.Data)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := findHTMLTitle(c); t != "" {
			return t
		}
	}
	return ""
}

// extractHTMLNodes walks the DOM tree and extracts headings and content blocks.
func extractHTMLNodes(n *html.Node, sections *[]Section) {
	if n.Type == html.ElementNode {
		// Skip boilerplate.
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Noscript, atom.Nav, atom.Footer, atom.Header:
			return
		}

		// Headings.
		switch n.DataAtom {
		case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			text := collectHTMLText(n)
			if text != "" {
				level := int(n.Data[1] - '0')
				*sections = append(*sections, Section{
					Title: text,
					Level: level,
					Text:  text,
					Type:  "heading",
				})
			}
			return

		case atom.P:
			text := collectHTMLText(n)
			if text != "" {
				*sections = append(*sections, Section{
					Text: text,
					Type: "paragraph",
				})
			}
			return

		case atom.Table:
			text := collectHTMLText(n)
			if text != "" {
				*sections = append(*sections, Section{
					Text: text,
					Type: "table",
				})
			}
			return

		case atom.Ul, atom.Ol:
			text := collectHTMLText(n)
			if text != "" {
				*sections = append(*sections, Section{
					Text: text,
					Type: "list",
				})
			}
			return
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractHTMLNodes(c, sections)
	}
}

// collectHTMLText extracts all visible text from a node subtree.
func collectHTMLText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if sb.Len() > 0 {
					sb.WriteByte(' ')
				}
				sb.WriteString(text)
			}
		}
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Noscript:
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}
