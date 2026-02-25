// CLAUDE:SUMMARY Main domregistry orchestrator — profile CRUD, correction submission with auto-accept, failure reporting, stats.
// Package domregistry is the community registry for shared DOM profiles.
//
// It centralises extraction profiles from all domkeeper instances. Profiles
// contain URL patterns, DOM behaviour data (landmarks, zones, fingerprint),
// and extraction strategies (CSS selectors per field). Contributing an
// extraction profile requires inspecting a page in devtools — no code needed.
//
// Flows:
//
//	Pull:  domkeeper starts crawling a new domain → queries registry → imports profile
//	Push:  domkeeper's auto-repair fixes an extractor → submits correction to registry
//	Report: an extractor fails locally → domkeeper reports failure → registry adjusts success_rate
//
// Usage:
//
//	r, err := domregistry.New(cfg, logger)
//	defer r.Close()
//	r.RegisterMCP(mcpServer)
//	r.RegisterConnectivity(router)
package domregistry

import (
	"context"
	"log/slog"

	"github.com/hazyhaar/chrc/domregistry/internal/store"
)

// Registry is the main domregistry orchestrator.
type Registry struct {
	store  *store.Store
	logger *slog.Logger
	config *Config
}

// New creates a Registry instance. Opens the SQLite database and initialises the schema.
func New(cfg *Config, logger *slog.Logger) (*Registry, error) {
	cfg.defaults()
	if logger == nil {
		logger = slog.Default()
	}

	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	return &Registry{
		store:  s,
		logger: logger,
		config: cfg,
	}, nil
}

// Close shuts down the registry and closes the database.
func (r *Registry) Close() error {
	return r.store.Close()
}

// Store returns the underlying store for direct access (testing, admin).
func (r *Registry) Store() *store.Store {
	return r.store
}

// --- Profile operations ---

// GetProfile retrieves a profile by ID.
func (r *Registry) GetProfile(ctx context.Context, id string) (*Profile, error) {
	return r.store.GetProfile(ctx, id)
}

// GetProfileByPattern retrieves a profile by URL pattern.
func (r *Registry) GetProfileByPattern(ctx context.Context, pattern string) (*Profile, error) {
	return r.store.GetProfileByPattern(ctx, pattern)
}

// SearchProfiles returns profiles for a domain.
func (r *Registry) SearchProfiles(ctx context.Context, domain string) ([]*Profile, error) {
	return r.store.ListProfilesByDomain(ctx, domain)
}

// ListProfiles returns profiles with optional trust level filter.
func (r *Registry) ListProfiles(ctx context.Context, trustLevel string, limit int) ([]*Profile, error) {
	return r.store.ListProfiles(ctx, trustLevel, limit)
}

// PublishProfile inserts or updates a profile in the registry.
func (r *Registry) PublishProfile(ctx context.Context, p *Profile) error {
	existing, err := r.store.GetProfileByPattern(ctx, p.URLPattern)
	if err != nil {
		return err
	}
	if existing != nil {
		p.ID = existing.ID
		return r.store.UpdateProfile(ctx, p)
	}
	return r.store.InsertProfile(ctx, p)
}

// DeleteProfile removes a profile.
func (r *Registry) DeleteProfile(ctx context.Context, id string) error {
	return r.store.DeleteProfile(ctx, id)
}

// --- Correction operations ---

// SubmitCorrection submits a correction proposal. If AutoAccept is enabled
// and the instance has sufficient reputation, the correction is auto-accepted.
func (r *Registry) SubmitCorrection(ctx context.Context, c *Correction) error {
	if err := r.store.InsertCorrection(ctx, c); err != nil {
		return err
	}

	if !r.config.AutoAccept {
		r.logger.Info("domregistry: correction submitted (manual review)",
			"correction_id", c.ID, "profile_id", c.ProfileID, "instance_id", c.InstanceID)
		return nil
	}

	autoAccept, err := r.store.ScoreCorrection(ctx, c.ID)
	if err != nil {
		r.logger.Warn("domregistry: scoring failed, correction stays pending",
			"correction_id", c.ID, "error", err)
		return nil // correction is inserted, scoring failure is non-fatal
	}

	if autoAccept {
		if err := r.store.AcceptCorrection(ctx, c.ID); err != nil {
			r.logger.Warn("domregistry: auto-accept failed",
				"correction_id", c.ID, "error", err)
			return nil
		}
		r.logger.Info("domregistry: correction auto-accepted",
			"correction_id", c.ID, "profile_id", c.ProfileID, "instance_id", c.InstanceID)
	} else {
		r.logger.Info("domregistry: correction pending review",
			"correction_id", c.ID, "profile_id", c.ProfileID, "instance_id", c.InstanceID)
	}

	return nil
}

// AcceptCorrection manually accepts a correction.
func (r *Registry) AcceptCorrection(ctx context.Context, correctionID string) error {
	return r.store.AcceptCorrection(ctx, correctionID)
}

// RejectCorrection manually rejects a correction.
func (r *Registry) RejectCorrection(ctx context.Context, correctionID string) error {
	return r.store.RejectCorrection(ctx, correctionID)
}

// ListPendingCorrections returns corrections awaiting review.
func (r *Registry) ListPendingCorrections(ctx context.Context, limit int) ([]*Correction, error) {
	return r.store.ListPendingCorrections(ctx, limit)
}

// --- Report operations ---

// ReportFailure submits a failure report for a profile.
func (r *Registry) ReportFailure(ctx context.Context, report *Report) error {
	if err := r.store.InsertReport(ctx, report); err != nil {
		return err
	}
	r.logger.Info("domregistry: failure reported",
		"profile_id", report.ProfileID, "instance_id", report.InstanceID, "error_type", report.ErrorType)
	return nil
}

// --- Stats ---

// Stats holds registry statistics.
type Stats struct {
	Profiles    int `json:"profiles"`
	Corrections int `json:"corrections"`
	Reports     int `json:"reports"`
}

// Stats returns registry statistics.
func (r *Registry) Stats(ctx context.Context) (*Stats, error) {
	profiles, err := r.store.CountProfiles(ctx)
	if err != nil {
		return nil, err
	}
	corrections, err := r.store.CountCorrections(ctx)
	if err != nil {
		return nil, err
	}
	reports, err := r.store.CountReports(ctx)
	if err != nil {
		return nil, err
	}
	return &Stats{
		Profiles:    profiles,
		Corrections: corrections,
		Reports:     reports,
	}, nil
}

// DomainLeaderboard returns aggregated domain statistics.
func (r *Registry) DomainLeaderboard(ctx context.Context, limit int) ([]*LeaderboardEntry, error) {
	return r.store.DomainLeaderboard(ctx, limit)
}

// InstanceLeaderboard returns instance reputation rankings.
func (r *Registry) InstanceLeaderboard(ctx context.Context, limit int) ([]*InstanceReputation, error) {
	return r.store.InstanceLeaderboard(ctx, limit)
}
