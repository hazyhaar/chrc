// CLAUDE:SUMMARY Registers domkeeper service handlers (search, rules, stats, GPU) on a connectivity Router.
package domkeeper

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hazyhaar/pkg/connectivity"
	"github.com/hazyhaar/chrc/domkeeper/internal/store"
	"github.com/hazyhaar/pkg/idgen"
)

// RegisterConnectivity registers domkeeper service handlers on a connectivity Router.
//
// Registered services:
//
//	domkeeper_search         — full-text search on extracted content
//	domkeeper_premium_search — tiered multi-pass search (free/premium)
//	domkeeper_add_rule       — create an extraction rule
//	domkeeper_list_rules     — list extraction rules
//	domkeeper_delete_rule    — delete an extraction rule
//	domkeeper_stats          — get domkeeper statistics
//	domkeeper_gpu_stats      — get GPU pricing and threshold data
//	domkeeper_gpu_threshold  — recompute GPU serverless vs dedicated decision
func (k *Keeper) RegisterConnectivity(router *connectivity.Router) {
	router.RegisterLocal("domkeeper_search", k.handleSearch)
	router.RegisterLocal("domkeeper_premium_search", k.handlePremiumSearch)
	router.RegisterLocal("domkeeper_add_rule", k.handleAddRule)
	router.RegisterLocal("domkeeper_list_rules", k.handleListRules)
	router.RegisterLocal("domkeeper_stats", k.handleStats)
	router.RegisterLocal("domkeeper_delete_rule", k.handleDeleteRule)
	router.RegisterLocal("domkeeper_gpu_stats", k.handleGPUStats)
	router.RegisterLocal("domkeeper_gpu_threshold", k.handleGPUThreshold)
}

func (k *Keeper) handleSearch(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		Query      string   `json:"query"`
		FolderIDs  []string `json:"folder_ids"`
		TrustLevel string   `json:"trust_level"`
		Limit      int      `json:"limit"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	results, err := k.Search(ctx, store.SearchOptions{
		Query:      req.Query,
		FolderIDs:  req.FolderIDs,
		TrustLevel: req.TrustLevel,
		Limit:      req.Limit,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(results)
}

func (k *Keeper) handleAddRule(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		Name        string   `json:"name"`
		URLPattern  string   `json:"url_pattern"`
		PageID      string   `json:"page_id"`
		Selectors   []string `json:"selectors"`
		ExtractMode string   `json:"extract_mode"`
		TrustLevel  string   `json:"trust_level"`
		FolderID    string   `json:"folder_id"`
		Priority    int      `json:"priority"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	mode := req.ExtractMode
	if mode == "" {
		mode = "auto"
	}
	trust := req.TrustLevel
	if trust == "" {
		trust = "unverified"
	}

	rule := &store.Rule{
		ID:          idgen.New(),
		Name:        req.Name,
		URLPattern:  req.URLPattern,
		PageID:      req.PageID,
		Selectors:   req.Selectors,
		ExtractMode: mode,
		TrustLevel:  trust,
		FolderID:    req.FolderID,
		Enabled:     true,
		Priority:    req.Priority,
	}
	if err := k.AddRule(ctx, rule); err != nil {
		return nil, err
	}
	return json.Marshal(rule)
}

func (k *Keeper) handleListRules(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		EnabledOnly bool `json:"enabled_only"`
	}
	json.Unmarshal(payload, &req) // OK if empty

	rules, err := k.ListRules(ctx, req.EnabledOnly)
	if err != nil {
		return nil, err
	}
	return json.Marshal(rules)
}

func (k *Keeper) handleStats(ctx context.Context, _ []byte) ([]byte, error) {
	stats, err := k.Stats(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(stats)
}

func (k *Keeper) handleDeleteRule(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		RuleID string `json:"rule_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := k.DeleteRule(ctx, req.RuleID); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"status": "deleted", "rule_id": req.RuleID})
}

func (k *Keeper) handlePremiumSearch(ctx context.Context, payload []byte) ([]byte, error) {
	var req PremiumSearchOptions
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	result, err := k.PremiumSearch(ctx, req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (k *Keeper) handleGPUStats(ctx context.Context, _ []byte) ([]byte, error) {
	stats, err := k.GPUStats(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(stats)
}

func (k *Keeper) handleGPUThreshold(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		BacklogUnits int `json:"backlog_units"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	threshold, err := k.ComputeGPUThreshold(ctx, req.BacklogUnits)
	if err != nil {
		return nil, err
	}
	return json.Marshal(threshold)
}
