// CLAUDE:SUMMARY Seed source catalog with curated collections (tech, legal-fr, opendata, academic, news-fr) and search engines.
// Package catalog provides seed source collections for veille spaces.
//
// Each category contains a curated set of sources (RSS, web, API) that can
// be bulk-inserted into a space for quick bootstrapping.
package catalog

import (
	"context"
	"fmt"
)

// SourceDef describes a source to be inserted.
type SourceDef struct {
	Name       string
	URL        string
	SourceType string // web, rss, api
	ConfigJSON string
	Interval   int64 // fetch_interval ms, default 3600000 (1h)
}

// Service provides AddSource for inserting sources.
type Service interface {
	AddSource(ctx context.Context, dossierID string, s *SourceInput) error
}

// SourceInput matches the veille.Source fields needed for insertion.
type SourceInput struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	SourceType    string `json:"source_type"`
	FetchInterval int64  `json:"fetch_interval"`
	Enabled       bool   `json:"enabled"`
	ConfigJSON    string `json:"config_json"`
}

// categories maps category names to their source definitions.
var categories = map[string][]SourceDef{
	"tech": {
		{Name: "Hacker News", URL: "https://hnrss.org/frontpage", SourceType: "rss", Interval: 1800000},
		{Name: "Lobsters", URL: "https://lobste.rs/rss", SourceType: "rss", Interval: 1800000},
		{Name: "Ars Technica", URL: "https://feeds.arstechnica.com/arstechnica/index", SourceType: "rss"},
		{Name: "The Verge", URL: "https://www.theverge.com/rss/index.xml", SourceType: "rss"},
		{Name: "TechCrunch", URL: "https://techcrunch.com/feed/", SourceType: "rss"},
		{Name: "Go Blog", URL: "https://go.dev/blog/feed.atom", SourceType: "rss", Interval: 86400000},
	},
	"legal-fr": {
		{Name: "Legifrance JORF", URL: "https://www.legifrance.gouv.fr/rss/jo", SourceType: "rss"},
		{Name: "CNIL Actualites", URL: "https://www.cnil.fr/fr/rss.xml", SourceType: "rss"},
		{Name: "EUR-Lex Recent", URL: "https://eur-lex.europa.eu/rss/cli-recent-acts.xml", SourceType: "rss", Interval: 86400000},
	},
	"opendata": {
		{Name: "data.gouv.fr Datasets", URL: "https://www.data.gouv.fr/api/1/datasets/?sort=-created&page_size=20", SourceType: "api",
			ConfigJSON: `{"result_path":"data","fields":{"title":"title","text":"description","url":"page"}}`, Interval: 86400000},
		{Name: "OpenAlex Works", URL: "https://api.openalex.org/works?sort=publication_date:desc&per_page=20", SourceType: "api",
			ConfigJSON: `{"result_path":"results","fields":{"title":"title","text":"abstract_inverted_index","url":"id"}}`, Interval: 86400000},
	},
	"academic": {
		{Name: "arXiv CS.AI", URL: "https://rss.arxiv.org/rss/cs.AI", SourceType: "rss", Interval: 86400000},
		{Name: "arXiv CS.CL", URL: "https://rss.arxiv.org/rss/cs.CL", SourceType: "rss", Interval: 86400000},
	},
	"news-fr": {
		{Name: "Le Monde Pixels", URL: "https://www.lemonde.fr/pixels/rss_full.xml", SourceType: "rss"},
		{Name: "Next INpact", URL: "https://www.nextinpact.com/rss/news.xml", SourceType: "rss"},
	},
}

// Categories returns the list of available category names.
func Categories() []string {
	names := make([]string, 0, len(categories))
	for name := range categories {
		names = append(names, name)
	}
	return names
}

// Sources returns the source definitions for a category.
func Sources(category string) ([]SourceDef, bool) {
	defs, ok := categories[category]
	return defs, ok
}

// Populate inserts all sources from a category into a veille space.
// Returns the number of sources inserted and skips duplicates by URL.
func Populate(ctx context.Context, addSource func(ctx context.Context, s *SourceInput) error, category string) (int, error) {
	defs, ok := categories[category]
	if !ok {
		return 0, fmt.Errorf("catalog: unknown category %q", category)
	}

	var count int
	for _, def := range defs {
		interval := def.Interval
		if interval == 0 {
			interval = 3600000 // 1h default
		}
		configJSON := def.ConfigJSON
		if configJSON == "" {
			configJSON = "{}"
		}

		src := &SourceInput{
			Name:          def.Name,
			URL:           def.URL,
			SourceType:    def.SourceType,
			FetchInterval: interval,
			Enabled:       true,
			ConfigJSON:    configJSON,
		}

		if err := addSource(ctx, src); err != nil {
			// Skip UNIQUE constraint violations (duplicate URLs).
			continue
		}
		count++
	}

	return count, nil
}

// SearchEngineDef describes a search engine to seed.
type SearchEngineDef struct {
	ID            string
	Name          string
	Strategy      string // "api" | "generic"
	URLTemplate   string
	APIConfigJSON string
	SelectorsJSON string
	StealthLevel  int
	RateLimitMs   int64
	MaxPages      int
	Enabled       bool
}

// SearchEngineInput matches the store.SearchEngine fields for insertion.
type SearchEngineInput struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Strategy      string `json:"strategy"`
	URLTemplate   string `json:"url_template"`
	APIConfigJSON string `json:"api_config"`
	SelectorsJSON string `json:"selectors"`
	StealthLevel  int    `json:"stealth_level"`
	RateLimitMs   int64  `json:"rate_limit_ms"`
	MaxPages      int    `json:"max_pages"`
	Enabled       bool   `json:"enabled"`
}

// defaultSearchEngines lists the seed search engines.
var defaultSearchEngines = []SearchEngineDef{
	{
		ID:          "brave_api",
		Name:        "Brave Search",
		Strategy:    "api",
		URLTemplate: "https://api.search.brave.com/res/v1/web/search?q={query}&count=20",
		APIConfigJSON: `{"headers":{"Accept":"application/json","Accept-Encoding":"gzip","X-Subscription-Token":"${BRAVE_API_KEY}"},"result_path":"web.results","fields":{"title":"title","text":"description","url":"url"}}`,
		RateLimitMs: 1000,
		MaxPages:    1,
		Enabled:     true,
	},
	{
		ID:            "ddg_html",
		Name:          "DuckDuckGo HTML",
		Strategy:      "generic",
		URLTemplate:   "https://html.duckduckgo.com/html/?q={query}",
		SelectorsJSON: `{"result_item":".result","title":".result__a","link":".result__a","snippet":".result__snippet"}`,
		StealthLevel:  2,
		RateLimitMs:   3000,
		MaxPages:      3,
		Enabled:       false, // stub — requires domwatch
	},
	{
		ID:          "github_search",
		Name:        "GitHub Search",
		Strategy:    "api",
		URLTemplate: "https://api.github.com/search/repositories?q={query}&sort=updated&per_page=20",
		APIConfigJSON: `{"headers":{"Accept":"application/vnd.github+json","Authorization":"Bearer ${GITHUB_TOKEN}"},"result_path":"items","fields":{"title":"full_name","text":"description","url":"html_url"}}`,
		RateLimitMs: 2000,
		MaxPages:    1,
		Enabled:     true,
	},
	{
		ID:            "scholar",
		Name:          "Google Scholar",
		Strategy:      "generic",
		URLTemplate:   "https://scholar.google.com/scholar?q={query}",
		SelectorsJSON: `{"result_item":".gs_r","title":".gs_rt a","link":".gs_rt a","snippet":".gs_rs"}`,
		StealthLevel:  3,
		RateLimitMs:   5000,
		MaxPages:      2,
		Enabled:       false, // stub — requires domwatch
	},
}

// PopulateSearchEngines inserts the default search engines into a shard.
// Returns the number inserted (skips duplicates by name).
func PopulateSearchEngines(ctx context.Context, insertFn func(ctx context.Context, e *SearchEngineInput) error) (int, error) {
	var count int
	for _, def := range defaultSearchEngines {
		apiConfig := def.APIConfigJSON
		if apiConfig == "" {
			apiConfig = "{}"
		}
		selectors := def.SelectorsJSON
		if selectors == "" {
			selectors = "{}"
		}

		e := &SearchEngineInput{
			ID:            def.ID,
			Name:          def.Name,
			Strategy:      def.Strategy,
			URLTemplate:   def.URLTemplate,
			APIConfigJSON: apiConfig,
			SelectorsJSON: selectors,
			StealthLevel:  def.StealthLevel,
			RateLimitMs:   def.RateLimitMs,
			MaxPages:      def.MaxPages,
			Enabled:       def.Enabled,
		}
		if err := insertFn(ctx, e); err != nil {
			// Skip UNIQUE constraint violations.
			continue
		}
		count++
	}
	return count, nil
}
