package domkeeper

import (
	"context"
	"log/slog"
	"time"

	"github.com/hazyhaar/chrc/domkeeper/internal/store"
	"github.com/hazyhaar/pkg/idgen"
)

// SearchTier controls the search quality level.
type SearchTier string

const (
	TierFree    SearchTier = "free"
	TierPremium SearchTier = "premium"
)

// PremiumSearchOptions extends SearchOptions with tier-specific settings.
type PremiumSearchOptions struct {
	Query      string     `json:"query"`
	FolderIDs  []string   `json:"folder_ids,omitempty"`
	TrustLevel string     `json:"trust_level,omitempty"`
	Limit      int        `json:"limit,omitempty"`
	Tier       SearchTier `json:"tier"`
	MaxPasses  int        `json:"max_passes,omitempty"` // premium: 3-5 passes
	UserID     string     `json:"user_id,omitempty"`
}

// SearchPass records the results and reasoning of one retrieval pass.
type SearchPass struct {
	PassNumber    int                `json:"pass_number"`
	Query         string             `json:"query"`
	Results       []*store.SearchResult `json:"results"`
	ResultCount   int                `json:"result_count"`
}

// PremiumSearchResult holds the full multi-pass search results.
type PremiumSearchResult struct {
	Passes      []SearchPass          `json:"passes"`
	Final       []*store.SearchResult `json:"results"`
	Tier        SearchTier            `json:"tier"`
	TotalPasses int                   `json:"total_passes"`
	LatencyMs   int64                 `json:"latency_ms"`
}

// PremiumSearch performs a tiered search. Free tier does a single FTS pass.
// Premium tier performs multi-pass retrieval with deduplication.
func (k *Keeper) PremiumSearch(ctx context.Context, opts PremiumSearchOptions) (*PremiumSearchResult, error) {
	start := time.Now()

	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if opts.Tier == "" {
		opts.Tier = TierFree
	}
	if opts.MaxPasses <= 0 {
		if opts.Tier == TierPremium {
			opts.MaxPasses = 3
		} else {
			opts.MaxPasses = 1
		}
	}

	result := &PremiumSearchResult{
		Tier: opts.Tier,
	}

	// Pass 1: initial search on the raw query.
	pass1Results, err := k.Search(ctx, store.SearchOptions{
		Query:      opts.Query,
		FolderIDs:  opts.FolderIDs,
		TrustLevel: opts.TrustLevel,
		Limit:      opts.Limit,
	})
	if err != nil {
		return nil, err
	}

	result.Passes = append(result.Passes, SearchPass{
		PassNumber:  1,
		Query:       opts.Query,
		Results:     pass1Results,
		ResultCount: len(pass1Results),
	})

	if opts.Tier == TierFree || opts.MaxPasses <= 1 {
		result.Final = pass1Results
		result.TotalPasses = 1
		result.LatencyMs = time.Since(start).Milliseconds()
		k.logSearchTier(ctx, opts, result)
		return result, nil
	}

	// Premium multi-pass: expand search with variant queries.
	seen := make(map[string]bool)
	var allResults []*store.SearchResult
	for _, r := range pass1Results {
		if !seen[r.ID] {
			seen[r.ID] = true
			allResults = append(allResults, r)
		}
	}

	// Generate additional passes using query expansion strategies.
	variants := expandQuery(opts.Query)
	passLimit := opts.MaxPasses
	if passLimit > len(variants)+1 {
		passLimit = len(variants) + 1
	}

	for i := 0; i < passLimit-1 && i < len(variants); i++ {
		variant := variants[i]
		passResults, err := k.Search(ctx, store.SearchOptions{
			Query:      variant,
			FolderIDs:  opts.FolderIDs,
			TrustLevel: opts.TrustLevel,
			Limit:      opts.Limit,
		})
		if err != nil {
			k.logger.Warn("premium search pass failed", "pass", i+2, "error", err)
			continue
		}

		for _, r := range passResults {
			if !seen[r.ID] {
				seen[r.ID] = true
				allResults = append(allResults, r)
			}
		}

		result.Passes = append(result.Passes, SearchPass{
			PassNumber:  i + 2,
			Query:       variant,
			Results:     passResults,
			ResultCount: len(passResults),
		})
	}

	// Apply trust_level boost to combined results.
	allResults = applyTrustBoost(allResults)

	// Trim to limit.
	if len(allResults) > opts.Limit {
		allResults = allResults[:opts.Limit]
	}

	result.Final = allResults
	result.TotalPasses = len(result.Passes)
	result.LatencyMs = time.Since(start).Milliseconds()

	k.logSearchTier(ctx, opts, result)
	return result, nil
}

// logSearchTier records the search in the tier log for analytics.
func (k *Keeper) logSearchTier(ctx context.Context, opts PremiumSearchOptions, result *PremiumSearchResult) {
	entry := &store.SearchTierLog{
		ID:           idgen.New(),
		UserID:       opts.UserID,
		Tier:         string(result.Tier),
		Query:        opts.Query,
		Passes:       result.TotalPasses,
		ResultsCount: len(result.Final),
		LatencyMs:    result.LatencyMs,
	}
	if err := k.store.InsertSearchTierLog(ctx, entry); err != nil {
		k.logger.Warn("failed to log search tier", "error", err, slog.String("tier", string(result.Tier)))
	}
}

// expandQuery generates query variants for multi-pass retrieval.
// Uses simple heuristic strategies: sub-queries, quoted phrases, term reordering.
func expandQuery(query string) []string {
	if len(query) == 0 {
		return nil
	}

	var variants []string

	// Strategy 1: quoted exact phrase
	variants = append(variants, `"`+query+`"`)

	// Strategy 2: individual significant terms (if multi-word)
	words := splitWords(query)
	if len(words) > 2 {
		// First half
		mid := len(words) / 2
		variants = append(variants, joinWords(words[:mid]))
		// Second half
		variants = append(variants, joinWords(words[mid:]))
	}

	return variants
}

// splitWords splits a query into words, filtering empty strings.
func splitWords(s string) []string {
	var words []string
	start := -1
	for i, c := range s {
		if c == ' ' || c == '\t' || c == '\n' {
			if start >= 0 {
				words = append(words, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		words = append(words, s[start:])
	}
	return words
}

// joinWords joins words with spaces.
func joinWords(words []string) string {
	if len(words) == 0 {
		return ""
	}
	result := words[0]
	for i := 1; i < len(words); i++ {
		result += " " + words[i]
	}
	return result
}

// applyTrustBoost reorders results by applying trust level multipliers.
// official: 1.3x, institutional: 1.1x, community: 1.0x, unverified: 0.8x
func applyTrustBoost(results []*store.SearchResult) []*store.SearchResult {
	type scored struct {
		result *store.SearchResult
		score  float64
	}

	trustMultiplier := map[string]float64{
		"official":      1.3,
		"institutional": 1.1,
		"community":     1.0,
		"unverified":    0.8,
	}

	scoredResults := make([]scored, len(results))
	for i, r := range results {
		mult := trustMultiplier[r.TrustLevel]
		if mult == 0 {
			mult = 1.0
		}
		// Use rank position as base score (lower index = higher score).
		scoredResults[i] = scored{
			result: r,
			score:  float64(len(results)-i) * mult,
		}
	}

	// Sort by score descending (simple insertion sort for typically small slices).
	for i := 1; i < len(scoredResults); i++ {
		for j := i; j > 0 && scoredResults[j].score > scoredResults[j-1].score; j-- {
			scoredResults[j], scoredResults[j-1] = scoredResults[j-1], scoredResults[j]
		}
	}

	out := make([]*store.SearchResult, len(scoredResults))
	for i, s := range scoredResults {
		out[i] = s.result
	}
	return out
}

// --- GPU monitoring ---

// GPUStats returns current GPU pricing and threshold status.
func (k *Keeper) GPUStats(ctx context.Context) (*GPUStatsResult, error) {
	pricing, err := k.store.ListGPUPricing(ctx, true)
	if err != nil {
		return nil, err
	}
	threshold, err := k.store.GetGPUThreshold(ctx)
	if err != nil {
		return nil, err
	}
	return &GPUStatsResult{
		Pricing:   pricing,
		Threshold: threshold,
	}, nil
}

// GPUStatsResult holds GPU monitoring data.
type GPUStatsResult struct {
	Pricing   []*store.GPUPricing   `json:"pricing"`
	Threshold *store.GPUThreshold   `json:"threshold"`
}

// ComputeGPUThreshold recalculates the serverless vs dedicated decision.
func (k *Keeper) ComputeGPUThreshold(ctx context.Context, backlogUnits int) (*store.GPUThreshold, error) {
	return k.store.ComputeGPUThreshold(ctx, backlogUnits)
}
