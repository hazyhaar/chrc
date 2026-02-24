package domregistry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hazyhaar/chrc/domregistry/internal/store"
	"github.com/hazyhaar/pkg/connectivity"
	"github.com/hazyhaar/pkg/idgen"
)

// RegisterConnectivity registers domregistry service handlers on a connectivity Router.
//
// Registered services:
//
//	domregistry_search_profiles   — search profiles by domain
//	domregistry_get_profile       — get a profile by ID
//	domregistry_publish_profile   — publish or update a profile
//	domregistry_submit_correction — submit an extractor correction
//	domregistry_report_failure    — report a profile failure
//	domregistry_leaderboard       — get domain leaderboard
//	domregistry_stats             — get registry statistics
func (r *Registry) RegisterConnectivity(router *connectivity.Router) {
	router.RegisterLocal("domregistry_search_profiles", r.handleSearchProfiles)
	router.RegisterLocal("domregistry_get_profile", r.handleGetProfile)
	router.RegisterLocal("domregistry_publish_profile", r.handlePublishProfile)
	router.RegisterLocal("domregistry_submit_correction", r.handleSubmitCorrection)
	router.RegisterLocal("domregistry_report_failure", r.handleReportFailure)
	router.RegisterLocal("domregistry_leaderboard", r.handleLeaderboard)
	router.RegisterLocal("domregistry_stats", r.handleStats)
}

func (r *Registry) handleSearchProfiles(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		Domain     string `json:"domain"`
		TrustLevel string `json:"trust_level"`
		Limit      int    `json:"limit"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	if req.Domain != "" {
		profiles, err := r.SearchProfiles(ctx, req.Domain)
		if err != nil {
			return nil, err
		}
		return json.Marshal(profiles)
	}
	profiles, err := r.ListProfiles(ctx, req.TrustLevel, req.Limit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(profiles)
}

func (r *Registry) handleGetProfile(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		ID         string `json:"id"`
		URLPattern string `json:"url_pattern"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	if req.ID != "" {
		p, err := r.GetProfile(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(p)
	}
	if req.URLPattern != "" {
		p, err := r.GetProfileByPattern(ctx, req.URLPattern)
		if err != nil {
			return nil, err
		}
		return json.Marshal(p)
	}
	return nil, fmt.Errorf("id or url_pattern required")
}

func (r *Registry) handlePublishProfile(ctx context.Context, payload []byte) ([]byte, error) {
	var p store.Profile
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if p.ID == "" {
		p.ID = idgen.New()
	}
	if err := r.PublishProfile(ctx, &p); err != nil {
		return nil, err
	}
	return json.Marshal(&p)
}

func (r *Registry) handleSubmitCorrection(ctx context.Context, payload []byte) ([]byte, error) {
	var c store.Correction
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if c.ID == "" {
		c.ID = idgen.New()
	}
	if err := r.SubmitCorrection(ctx, &c); err != nil {
		return nil, err
	}
	// Re-fetch to get validated status (may have been auto-accepted)
	updated, err := r.store.GetCorrection(ctx, c.ID)
	if err != nil {
		return json.Marshal(&c)
	}
	return json.Marshal(updated)
}

func (r *Registry) handleReportFailure(ctx context.Context, payload []byte) ([]byte, error) {
	var rpt store.Report
	if err := json.Unmarshal(payload, &rpt); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if rpt.ID == "" {
		rpt.ID = idgen.New()
	}
	if err := r.ReportFailure(ctx, &rpt); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"status": "reported", "report_id": rpt.ID})
}

func (r *Registry) handleLeaderboard(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		Type  string `json:"type"` // "domain" or "instance"
		Limit int    `json:"limit"`
	}
	json.Unmarshal(payload, &req)

	if req.Limit <= 0 {
		req.Limit = 50
	}

	switch req.Type {
	case "instance":
		reps, err := r.InstanceLeaderboard(ctx, req.Limit)
		if err != nil {
			return nil, err
		}
		return json.Marshal(reps)
	default: // "domain" or empty
		entries, err := r.DomainLeaderboard(ctx, req.Limit)
		if err != nil {
			return nil, err
		}
		return json.Marshal(entries)
	}
}

func (r *Registry) handleStats(ctx context.Context, _ []byte) ([]byte, error) {
	stats, err := r.Stats(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(stats)
}
