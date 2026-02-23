package profiler

import (
	"context"
	"encoding/json"

	"github.com/hazyhaar/pkg/domwatch/internal/browser"
)

// computeTextDensity calculates the text-to-markup ratio for major DOM subtrees.
func computeTextDensity(tab *browser.Tab, ctx context.Context) map[string]float64 {
	script := `() => {
		function textLen(el) {
			let len = 0;
			const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
			while (walker.nextNode()) {
				len += walker.currentNode.textContent.trim().length;
			}
			return len;
		}

		function markupLen(el) {
			return el.innerHTML.length - textLen(el);
		}

		function xpath(el) {
			const parts = [];
			let node = el;
			while (node && node.nodeType === 1) {
				let idx = 0;
				let sib = node.previousElementSibling;
				while (sib) {
					if (sib.tagName === node.tagName) idx++;
					sib = sib.previousElementSibling;
				}
				const t = node.tagName.toLowerCase();
				parts.unshift(idx > 0 ? t + '[' + (idx+1) + ']' : t);
				node = node.parentElement;
			}
			return '/' + parts.join('/');
		}

		const result = {};
		// Analyse depth-1 children of body.
		const body = document.body;
		if (!body) return JSON.stringify(result);

		for (const child of body.children) {
			const tl = textLen(child);
			const ml = markupLen(child);
			const total = tl + ml;
			if (total > 0) {
				result[xpath(child)] = Math.round((tl / total) * 1000) / 1000;
			}
		}
		return JSON.stringify(result);
	}`

	res, err := tab.Page.Context(ctx).Eval(script)
	if err != nil {
		return nil
	}

	var density map[string]float64
	json.Unmarshal([]byte(res.Value.Str()), &density)
	return density
}
