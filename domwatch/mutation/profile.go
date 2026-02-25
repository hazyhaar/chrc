// CLAUDE:SUMMARY Defines Profile, Landmark, and Zone types for structural DOM analysis results.
package mutation

// Profile is a structural analysis of a page's DOM, produced on first visit.
// domkeeper consumes it to bootstrap extraction rules.
type Profile struct {
	PageURL          string             `json:"page_url"`
	Landmarks        []Landmark         `json:"landmarks"`
	DynamicZones     []Zone             `json:"dynamic_zones"`
	StaticZones      []Zone             `json:"static_zones"`
	ContentSelectors []string           `json:"content_selectors"`
	Fingerprint      string             `json:"fingerprint"`       // structural hash
	TextDensityMap   map[string]float64 `json:"text_density_map"` // XPath → text/markup ratio
}

// Landmark is an HTML5 landmark element found in the DOM.
type Landmark struct {
	Tag   string `json:"tag"`   // main, article, section, nav, header, footer, aside
	XPath string `json:"xpath"`
	Role  string `json:"role,omitempty"` // ARIA role if present
}

// Zone is a region of the DOM classified as dynamic or static.
type Zone struct {
	XPath        string  `json:"xpath"`
	Selector     string  `json:"selector"`      // shortest unique CSS selector
	MutationRate float64 `json:"mutation_rate"` // mutations/second (0 for static zones)
}
