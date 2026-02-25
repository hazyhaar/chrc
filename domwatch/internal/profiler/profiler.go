// CLAUDE:SUMMARY Orchestrates DOM structural profiling: landmarks, density, fingerprint, content selectors, and dynamic zones.
// Package profiler analyses the structural properties of a page's DOM.
// It produces a mutation.Profile that domkeeper uses to bootstrap
// extraction rules.
package profiler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hazyhaar/chrc/domwatch/internal/browser"
	"github.com/hazyhaar/chrc/domwatch/mutation"
)

// Config for profiling.
type Config struct {
	// ObserveDuration is how long to watch for mutations. Default: 10s.
	ObserveDuration time.Duration
	Logger          *slog.Logger
}

func (c *Config) defaults() {
	if c.ObserveDuration <= 0 {
		c.ObserveDuration = 10 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Profile analyses a page and returns its structural profile.
func Profile(ctx context.Context, tab *browser.Tab, cfg Config) (*mutation.Profile, error) {
	cfg.defaults()
	log := cfg.Logger

	log.Info("profiler: starting", "url", tab.PageURL, "observe_duration", cfg.ObserveDuration)

	// Step 1: Get the DOM HTML for structural analysis.
	html, err := tab.GetFullDOM(ctx)
	if err != nil {
		return nil, err
	}

	// Step 2: Find landmarks.
	landmarks := findLandmarks(tab, ctx)

	// Step 3: Compute text density map.
	densityMap := computeTextDensity(tab, ctx)

	// Step 4: Generate content selectors.
	contentSels := findContentSelectors(tab, ctx)

	// Step 5: Compute structural fingerprint.
	fp := computeFingerprint(html)

	// Step 6: Observe mutations to classify dynamic vs static zones.
	dynamicZones, staticZones := observeZones(ctx, tab, cfg.ObserveDuration)

	prof := &mutation.Profile{
		PageURL:          tab.PageURL,
		Landmarks:        landmarks,
		DynamicZones:     dynamicZones,
		StaticZones:      staticZones,
		ContentSelectors: contentSels,
		Fingerprint:      fp,
		TextDensityMap:   densityMap,
	}

	log.Info("profiler: complete",
		"url", tab.PageURL,
		"landmarks", len(landmarks),
		"dynamic", len(dynamicZones),
		"static", len(staticZones),
		"selectors", len(contentSels))

	return prof, nil
}

// observeZones watches mutations for the given duration and classifies
// DOM zones as dynamic (mutating) or static.
func observeZones(ctx context.Context, tab *browser.Tab, dur time.Duration) (dynamic, static []mutation.Zone) {
	// Inject a temporary MutationObserver that counts mutations per subtree.
	countScript := `() => {
		return new Promise((resolve) => {
			const counts = {};
			const obs = new MutationObserver((mutations) => {
				for (const m of mutations) {
					const target = m.target;
					// Find the nearest element with an ID or semantic tag.
					let el = target.nodeType === 1 ? target : target.parentElement;
					if (!el) continue;

					// Walk up to find a good anchor.
					while (el && !el.id && !['main','article','section','nav','header','footer','aside'].includes(el.tagName.toLowerCase())) {
						el = el.parentElement;
					}
					if (!el) el = target.nodeType === 1 ? target : target.parentElement;
					if (!el) continue;

					const key = el.id ? '#' + el.id : el.tagName.toLowerCase();
					counts[key] = (counts[key] || 0) + 1;
				}
			});
			obs.observe(document.documentElement, {
				childList: true, attributes: true, characterData: true,
				subtree: true
			});
			setTimeout(() => {
				obs.disconnect();
				resolve(JSON.stringify(counts));
			}, ` + fmt.Sprintf("%d", dur.Milliseconds()) + `);
		});
	}`

	res, err := tab.Page.Context(ctx).Eval(countScript)
	if err != nil {
		return nil, nil
	}

	var counts map[string]int
	if err := json.Unmarshal([]byte(res.Value.Str()), &counts); err != nil {
		return nil, nil
	}

	seconds := dur.Seconds()
	for sel, count := range counts {
		rate := float64(count) / seconds
		if rate > 0.1 {
			dynamic = append(dynamic, mutation.Zone{
				Selector:     sel,
				XPath:        "", // Would need JS evaluation to get XPath
				MutationRate: rate,
			})
		}
	}

	// Static zones: landmarks that didn't show up in mutation counts.
	staticSels := []string{"nav", "header", "footer", "aside"}
	for _, tag := range staticSels {
		if _, found := counts[tag]; !found {
			static = append(static, mutation.Zone{
				Selector:     tag,
				MutationRate: 0,
			})
		}
	}

	return dynamic, static
}
