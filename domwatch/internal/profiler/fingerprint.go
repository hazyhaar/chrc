package profiler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hazyhaar/pkg/domwatch/internal/browser"
)

// computeFingerprint generates a structural hash of the DOM: tags + depth +
// child counts, ignoring text content. This detects structural changes
// between crawls without being affected by content updates.
func computeFingerprint(html []byte) string {
	// Simple approach: extract the skeleton (tags only, no attributes/text).
	skeleton := extractSkeleton(html)
	h := sha256.Sum256([]byte(skeleton))
	return fmt.Sprintf("%x", h[:16]) // 128-bit fingerprint is enough
}

// extractSkeleton strips all text content and attributes, leaving only
// the tag structure with nesting depth.
func extractSkeleton(html []byte) string {
	var b strings.Builder
	inTag := false
	inAttr := false
	tagName := strings.Builder{}
	isClosing := false
	depth := 0

	for i := 0; i < len(html); i++ {
		ch := html[i]

		if ch == '<' {
			inTag = true
			inAttr = false
			tagName.Reset()
			isClosing = false
			if i+1 < len(html) && html[i+1] == '/' {
				isClosing = true
				i++ // skip /
			}
			continue
		}

		if inTag {
			if ch == '>' {
				inTag = false
				name := strings.ToLower(tagName.String())
				if name == "" || name == "!" || name[0] == '?' {
					continue
				}
				// Skip self-closing void elements.
				if isVoidElement(name) {
					fmt.Fprintf(&b, "%d:%s;", depth, name)
					continue
				}
				if isClosing {
					depth--
					if depth < 0 {
						depth = 0
					}
				} else {
					fmt.Fprintf(&b, "%d:%s;", depth, name)
					depth++
				}
			} else if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
				inAttr = true
			} else if !inAttr {
				tagName.WriteByte(ch)
			}
			continue
		}
	}

	return b.String()
}

func isVoidElement(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

// findContentSelectors generates CSS selectors for high text-density zones.
func findContentSelectors(tab *browser.Tab, ctx context.Context) []string {
	script := `() => {
		function textLen(el) {
			let len = 0;
			const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
			while (walker.nextNode()) {
				len += walker.currentNode.textContent.trim().length;
			}
			return len;
		}

		function getSelector(el) {
			if (el.id) return '#' + el.id;
			const tag = el.tagName.toLowerCase();
			const cls = Array.from(el.classList).filter(c => c.length < 30).slice(0, 2);
			if (cls.length > 0) return tag + '.' + cls.join('.');
			return tag;
		}

		const candidates = [];
		// Look at semantic containers first.
		const semantic = ['main', 'article', '[role="main"]', '[role="article"]'];
		for (const sel of semantic) {
			const els = document.querySelectorAll(sel);
			for (const el of els) {
				const tl = textLen(el);
				if (tl > 100) {
					candidates.push(getSelector(el));
				}
			}
		}

		// If no semantic containers found, look at divs with high text density.
		if (candidates.length === 0) {
			const divs = document.querySelectorAll('div');
			for (const div of divs) {
				const tl = textLen(div);
				const total = div.innerHTML.length;
				if (total > 0 && tl > 200 && (tl / total) > 0.3) {
					candidates.push(getSelector(div));
				}
			}
		}

		return JSON.stringify(candidates.slice(0, 10));
	}`

	res, err := tab.Page.Context(ctx).Eval(script)
	if err != nil {
		return nil
	}

	var sels []string
	json.Unmarshal([]byte(res.Value.Str()), &sels)
	return sels
}
