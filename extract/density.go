package extract

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// extractDensity extracts content using text density analysis.
// It identifies the DOM subtree with the highest text-to-markup ratio,
// filtering out boilerplate (nav, footer, sidebar, ads).
func extractDensity(doc *html.Node, title string, minLen int) (*Result, error) {
	// First try semantic landmarks.
	landmarks := findContentByLandmarks(doc)
	if len(landmarks) > 0 {
		var allText []string
		var allHTML []string
		for _, n := range landmarks {
			if isBoilerplate(n) {
				continue
			}
			text := collectText(n)
			if len(text) >= minLen {
				allText = append(allText, text)
				allHTML = append(allHTML, renderNode(n))
			}
		}
		if len(allText) > 0 {
			combined := strings.Join(allText, "\n\n")
			return &Result{
				Text:  combined,
				HTML:  strings.Join(allHTML, "\n"),
				Title: title,
				Hash:  hashText(combined),
			}, nil
		}
	}

	// Fall back to density scoring on the body.
	body := findBody(doc)
	if body == nil {
		body = doc
	}

	best := findDensestNode(body, minLen)
	if best == nil {
		// Last resort: collect all text from body.
		text := collectCleanText(body)
		if len(text) < minLen {
			return &Result{Title: title, Hash: hashText("")}, nil
		}
		return &Result{
			Text:  text,
			HTML:  renderNode(body),
			Title: title,
			Hash:  hashText(text),
		}, nil
	}

	text := collectText(best)
	return &Result{
		Text:  text,
		HTML:  renderNode(best),
		Title: title,
		Hash:  hashText(text),
	}, nil
}

// nodeScore holds density analysis for a DOM subtree.
type nodeScore struct {
	node       *html.Node
	textLen    int
	markupLen  int
	density    float64
	depth      int
	linkDens   float64 // fraction of text inside <a> tags
}

// findDensestNode walks the DOM and finds the node with highest content density.
func findDensestNode(root *html.Node, minLen int) *html.Node {
	var candidates []nodeScore

	var walk func(*html.Node, int)
	walk = func(n *html.Node, depth int) {
		if n.Type != html.ElementNode {
			return
		}
		if isBoilerplate(n) {
			return
		}
		if !isContentTag(n.DataAtom) && n.DataAtom != atom.Body {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, depth+1)
			}
			return
		}

		text := collectText(n)
		textLen := len(text)
		if textLen < minLen {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, depth+1)
			}
			return
		}

		markup := renderNode(n)
		markupLen := len(markup)
		if markupLen == 0 {
			markupLen = 1
		}

		linkText := collectLinkText(n)
		linkDens := float64(len(linkText)) / float64(textLen)

		density := float64(textLen) / float64(markupLen)

		candidates = append(candidates, nodeScore{
			node:      n,
			textLen:   textLen,
			markupLen: markupLen,
			density:   density,
			depth:     depth,
			linkDens:  linkDens,
		})

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, depth+1)
		}
	}

	walk(root, 0)

	if len(candidates) == 0 {
		return nil
	}

	// Score candidates: high density + low link density + reasonable text length.
	var best *nodeScore
	var bestScore float64

	for i := range candidates {
		c := &candidates[i]
		if c.linkDens > 0.5 {
			continue // mostly links - probably navigation
		}

		// Composite score: density * log(textLen) * (1 - linkDensity)
		score := c.density * logScale(c.textLen) * (1 - c.linkDens)

		if score > bestScore {
			bestScore = score
			best = c
		}
	}

	if best == nil {
		return nil
	}
	return best.node
}

// logScale returns a log-based scale factor for text length.
func logScale(n int) float64 {
	if n <= 0 {
		return 0
	}
	// Simple log2-ish scaling.
	scale := 1.0
	v := n
	for v > 100 {
		scale += 1
		v /= 2
	}
	return scale
}

// collectLinkText extracts text only from <a> elements.
func collectLinkText(n *html.Node) string {
	var sb strings.Builder
	var f func(*html.Node, bool)
	f = func(n *html.Node, inLink bool) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			inLink = true
		}
		if n.Type == html.TextNode && inLink {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				sb.WriteString(text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c, inLink)
		}
	}
	f(n, false)
	return sb.String()
}

// collectCleanText extracts text excluding boilerplate regions.
func collectCleanText(n *html.Node) string {
	var sb strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && isBoilerplate(n) {
			return
		}
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Noscript:
				return
			}
		}
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if sb.Len() > 0 {
					sb.WriteByte(' ')
				}
				sb.WriteString(text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return sb.String()
}

// findBody returns the <body> element from a parsed document.
func findBody(doc *html.Node) *html.Node {
	var body *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Body {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return body
}
