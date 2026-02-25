// CLAUDE:SUMMARY Detects HTML5 landmark elements (main, article, section, nav, header, footer, aside) via JS evaluation.
package profiler

import (
	"context"
	"encoding/json"

	"github.com/hazyhaar/chrc/domwatch/internal/browser"
	"github.com/hazyhaar/chrc/domwatch/mutation"
)

// findLandmarks detects HTML5 landmark elements in the DOM.
func findLandmarks(tab *browser.Tab, ctx context.Context) []mutation.Landmark {
	script := `() => {
		const tags = ['main', 'article', 'section', 'nav', 'header', 'footer', 'aside'];
		const results = [];
		for (const tag of tags) {
			const els = document.querySelectorAll(tag);
			for (const el of els) {
				const parts = [];
				let node = el;
				while (node && node.nodeType === 1) {
					let idx = 0;
					let sibling = node.previousElementSibling;
					while (sibling) {
						if (sibling.tagName === node.tagName) idx++;
						sibling = sibling.previousElementSibling;
					}
					const t = node.tagName.toLowerCase();
					parts.unshift(idx > 0 ? t + '[' + (idx+1) + ']' : t);
					node = node.parentElement;
				}
				results.push({
					tag: tag,
					xpath: '/' + parts.join('/'),
					role: el.getAttribute('role') || ''
				});
			}
		}
		return JSON.stringify(results);
	}`

	res, err := tab.Page.Context(ctx).Eval(script)
	if err != nil {
		return nil
	}

	var landmarks []mutation.Landmark
	json.Unmarshal([]byte(res.Value.Str()), &landmarks)
	return landmarks
}
