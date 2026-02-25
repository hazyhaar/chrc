// CLAUDE:SUMMARY CSS selector-based content extraction from parsed HTML documents.
package extract

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// extractCSS extracts content matching CSS selectors.
// Supports a subset of CSS selectors:
//   - tag: "article", "main", "div"
//   - .class: ".content", ".article-body"
//   - #id: "#main-content"
//   - tag.class: "div.content"
//   - tag#id: "div#main"
//   - tag[attr]: "div[data-content]"
//   - tag[attr=val]: "div[role=main]"
//   - combinations separated by space (descendant combinator)
func extractCSS(doc *html.Node, selectors []string, title string, minLen int) (*Result, error) {
	var allText []string
	var allHTML []string

	for _, sel := range selectors {
		matches := querySelectorAll(doc, sel)
		for _, n := range matches {
			text := collectText(n)
			if len(text) >= minLen {
				allText = append(allText, text)
				allHTML = append(allHTML, renderNode(n))
			}
		}
	}

	if len(allText) == 0 {
		return nil, fmt.Errorf("no content matched selectors: %v", selectors)
	}

	combined := strings.Join(allText, "\n\n")
	return &Result{
		Text:  combined,
		HTML:  strings.Join(allHTML, "\n"),
		Title: title,
		Hash:  hashText(combined),
	}, nil
}

// querySelectorAll returns all nodes matching a simple CSS selector.
func querySelectorAll(doc *html.Node, selector string) []*html.Node {
	parts := strings.Fields(selector)
	if len(parts) == 0 {
		return nil
	}

	// Start with all nodes matching the first part.
	matches := matchSimple(doc, parts[0])

	// For descendant combinators, filter through subsequent parts.
	for i := 1; i < len(parts); i++ {
		var nextMatches []*html.Node
		for _, parent := range matches {
			nextMatches = append(nextMatches, matchSimple(parent, parts[i])...)
		}
		matches = nextMatches
	}

	return matches
}

// matchSimple finds all nodes matching a single CSS selector part.
func matchSimple(root *html.Node, sel string) []*html.Node {
	m := parseSimpleSelector(sel)
	var results []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if matchesSelector(n, m) {
			results = append(results, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return results
}

type simpleSelector struct {
	tag      string
	id       string
	class    string
	attrKey  string
	attrVal  string
}

// parseSimpleSelector parses "tag.class", "#id", "tag[attr=val]", etc.
func parseSimpleSelector(sel string) simpleSelector {
	var s simpleSelector

	// Handle attribute selector: tag[attr] or tag[attr=val]
	if idx := strings.IndexByte(sel, '['); idx >= 0 {
		attrPart := strings.TrimRight(sel[idx+1:], "]")
		sel = sel[:idx]
		if eqIdx := strings.IndexByte(attrPart, '='); eqIdx >= 0 {
			s.attrKey = attrPart[:eqIdx]
			s.attrVal = strings.Trim(attrPart[eqIdx+1:], `"'`)
		} else {
			s.attrKey = attrPart
		}
	}

	// Handle #id
	if idx := strings.IndexByte(sel, '#'); idx >= 0 {
		s.id = sel[idx+1:]
		sel = sel[:idx]
	}

	// Handle .class
	if idx := strings.IndexByte(sel, '.'); idx >= 0 {
		s.class = sel[idx+1:]
		sel = sel[:idx]
	}

	s.tag = sel
	return s
}

// matchesSelector checks if a node matches a parsed simple selector.
func matchesSelector(n *html.Node, s simpleSelector) bool {
	if n.Type != html.ElementNode {
		return false
	}

	if s.tag != "" && n.Data != s.tag {
		return false
	}

	if s.id != "" {
		if getAttr(n, "id") != s.id {
			return false
		}
	}

	if s.class != "" {
		classes := strings.Fields(getAttr(n, "class"))
		found := false
		for _, c := range classes {
			if c == s.class {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if s.attrKey != "" {
		val := getAttr(n, s.attrKey)
		if s.attrVal != "" {
			if val != s.attrVal {
				return false
			}
		} else {
			if !hasAttr(n, s.attrKey) {
				return false
			}
		}
	}

	return true
}

// getAttr returns the value of an attribute on a node.
func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// hasAttr checks if a node has a specific attribute.
func hasAttr(n *html.Node, key string) bool {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

// findContentByLandmarks tries to find content in semantic HTML5 elements.
func findContentByLandmarks(doc *html.Node) []*html.Node {
	landmarks := []atom.Atom{atom.Main, atom.Article}
	for _, tag := range landmarks {
		nodes := findAllByTag(doc, tag)
		if len(nodes) > 0 {
			return nodes
		}
	}
	return nil
}

// findAllByTag finds all elements with a specific tag.
func findAllByTag(root *html.Node, tag atom.Atom) []*html.Node {
	var results []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == tag {
			results = append(results, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return results
}
