package extract

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// extractXPath extracts content matching XPath expressions.
// Supports a practical subset of XPath:
//   - /html/body/div      — absolute path
//   - //article           — descendant anywhere
//   - //div[@class='x']   — attribute predicate
//   - //div[2]            — positional predicate
//   - /html/body/main/p   — chained absolute path
func extractXPath(doc *html.Node, xpaths []string, title string, minLen int) (*Result, error) {
	var allText []string
	var allHTML []string

	for _, xp := range xpaths {
		matches := evaluateXPath(doc, xp)
		for _, n := range matches {
			text := collectText(n)
			if len(text) >= minLen {
				allText = append(allText, text)
				allHTML = append(allHTML, renderNode(n))
			}
		}
	}

	if len(allText) == 0 {
		return nil, fmt.Errorf("no content matched XPath: %v", xpaths)
	}

	combined := strings.Join(allText, "\n\n")
	return &Result{
		Text:  combined,
		HTML:  strings.Join(allHTML, "\n"),
		Title: title,
		Hash:  hashText(combined),
	}, nil
}

// evaluateXPath evaluates a simple XPath and returns matching nodes.
func evaluateXPath(doc *html.Node, xpath string) []*html.Node {
	xpath = strings.TrimSpace(xpath)

	// Handle // (descendant-or-self) prefix.
	if strings.HasPrefix(xpath, "//") {
		return findDescendants(doc, xpath[2:])
	}

	// Handle / (absolute) path.
	if strings.HasPrefix(xpath, "/") {
		return followAbsolutePath(doc, xpath[1:])
	}

	// Bare expression — treat as descendant search.
	return findDescendants(doc, xpath)
}

// findDescendants finds all elements matching the first step of a //expr path.
func findDescendants(root *html.Node, expr string) []*html.Node {
	// Split on / for multi-step paths: //div/p → find div anywhere, then p children.
	steps := strings.SplitN(expr, "/", 2)
	step := steps[0]
	tag, pred := parseXPathStep(step)

	var matches []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if matchesXPathStep(n, tag, pred) {
			matches = append(matches, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	// Continue with remaining path steps.
	if len(steps) > 1 && steps[1] != "" {
		var filtered []*html.Node
		for _, m := range matches {
			filtered = append(filtered, followRelativePath(m, steps[1])...)
		}
		return filtered
	}

	return matches
}

// followAbsolutePath follows a /step/step/... path from root.
func followAbsolutePath(root *html.Node, path string) []*html.Node {
	steps := strings.Split(path, "/")
	current := []*html.Node{root}

	for _, step := range steps {
		if step == "" {
			continue
		}
		tag, pred := parseXPathStep(step)
		var next []*html.Node
		for _, parent := range current {
			for c := parent.FirstChild; c != nil; c = c.NextSibling {
				if matchesXPathStep(c, tag, pred) {
					next = append(next, c)
				}
			}
		}
		current = next
	}

	return current
}

// followRelativePath follows a relative path from a node.
func followRelativePath(node *html.Node, path string) []*html.Node {
	steps := strings.Split(path, "/")
	current := []*html.Node{node}

	for _, step := range steps {
		if step == "" {
			continue
		}
		tag, pred := parseXPathStep(step)
		var next []*html.Node
		for _, parent := range current {
			for c := parent.FirstChild; c != nil; c = c.NextSibling {
				if matchesXPathStep(c, tag, pred) {
					next = append(next, c)
				}
			}
		}
		current = next
	}

	return current
}

type xpathPredicate struct {
	attrName  string
	attrValue string
	position  int // 1-based
}

// parseXPathStep parses "div", "div[@class='x']", "div[2]".
func parseXPathStep(step string) (string, *xpathPredicate) {
	idx := strings.IndexByte(step, '[')
	if idx < 0 {
		return step, nil
	}

	tag := step[:idx]
	predStr := strings.TrimRight(step[idx+1:], "]")

	pred := &xpathPredicate{}

	// Positional: [2]
	if n, err := strconv.Atoi(predStr); err == nil {
		pred.position = n
		return tag, pred
	}

	// Attribute: [@class='value'] or [@data-x]
	if strings.HasPrefix(predStr, "@") {
		attrExpr := predStr[1:]
		if eqIdx := strings.IndexByte(attrExpr, '='); eqIdx >= 0 {
			pred.attrName = attrExpr[:eqIdx]
			pred.attrValue = strings.Trim(attrExpr[eqIdx+1:], `'"`)
		} else {
			pred.attrName = attrExpr
		}
		return tag, pred
	}

	return tag, nil
}

// matchesXPathStep checks if a node matches a tag + optional predicate.
func matchesXPathStep(n *html.Node, tag string, pred *xpathPredicate) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if tag != "*" && n.Data != tag {
		return false
	}

	if pred == nil {
		return true
	}

	if pred.attrName != "" {
		val := getAttr(n, pred.attrName)
		if pred.attrValue != "" {
			return val == pred.attrValue
		}
		return hasAttr(n, pred.attrName)
	}

	if pred.position > 0 {
		// Count sibling elements with the same tag.
		pos := 0
		for s := n.Parent.FirstChild; s != nil; s = s.NextSibling {
			if s.Type == html.ElementNode && s.Data == n.Data {
				pos++
				if s == n {
					return pos == pred.position
				}
			}
		}
		return false
	}

	return true
}
