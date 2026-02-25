// CLAUDE:SUMMARY CRUD for community DOM profiles — insert, search by domain, EMA success tracking, contributor management.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Profile is a community DOM profile for a URL pattern.
type Profile struct {
	ID           string   `json:"id"`
	URLPattern   string   `json:"url_pattern"`
	Domain       string   `json:"domain"`
	SchemaID     string   `json:"schema_id,omitempty"`
	Extractors   string   `json:"extractors"`   // JSON: extraction strategy + selectors
	DOMProfile   string   `json:"dom_profile"`  // JSON: landmarks, zones, fingerprint
	TrustLevel   string   `json:"trust_level"`  // "official", "institutional", "community"
	SuccessRate  float64  `json:"success_rate"`
	TotalUses    int      `json:"total_uses"`
	TotalRepairs int      `json:"total_repairs"`
	Contributors []string `json:"contributors"` // instance IDs
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
}

// InsertProfile inserts a new community profile.
func (s *Store) InsertProfile(ctx context.Context, p *Profile) error {
	contribs, _ := json.Marshal(p.Contributors)
	now := time.Now().UnixMilli()
	if p.CreatedAt == 0 {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO profiles
			(id, url_pattern, domain, schema_id, extractors, dom_profile, trust_level,
			 success_rate, total_uses, total_repairs, contributors, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.URLPattern, p.Domain, p.SchemaID, p.Extractors, p.DOMProfile, p.TrustLevel,
		p.SuccessRate, p.TotalUses, p.TotalRepairs, string(contribs), p.CreatedAt, p.UpdatedAt,
	)
	return err
}

// GetProfile retrieves a profile by ID.
func (s *Store) GetProfile(ctx context.Context, id string) (*Profile, error) {
	p := &Profile{}
	var contribs string

	err := s.DB.QueryRowContext(ctx, `
		SELECT id, url_pattern, domain, schema_id, extractors, dom_profile, trust_level,
		       success_rate, total_uses, total_repairs, contributors, created_at, updated_at
		FROM profiles WHERE id = ?`, id).Scan(
		&p.ID, &p.URLPattern, &p.Domain, &p.SchemaID, &p.Extractors, &p.DOMProfile, &p.TrustLevel,
		&p.SuccessRate, &p.TotalUses, &p.TotalRepairs, &contribs, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(contribs), &p.Contributors)
	return p, nil
}

// GetProfileByPattern retrieves a profile by URL pattern.
func (s *Store) GetProfileByPattern(ctx context.Context, pattern string) (*Profile, error) {
	p := &Profile{}
	var contribs string

	err := s.DB.QueryRowContext(ctx, `
		SELECT id, url_pattern, domain, schema_id, extractors, dom_profile, trust_level,
		       success_rate, total_uses, total_repairs, contributors, created_at, updated_at
		FROM profiles WHERE url_pattern = ?`, pattern).Scan(
		&p.ID, &p.URLPattern, &p.Domain, &p.SchemaID, &p.Extractors, &p.DOMProfile, &p.TrustLevel,
		&p.SuccessRate, &p.TotalUses, &p.TotalRepairs, &contribs, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(contribs), &p.Contributors)
	return p, nil
}

// ListProfilesByDomain returns profiles matching the given domain.
func (s *Store) ListProfilesByDomain(ctx context.Context, domain string) ([]*Profile, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, url_pattern, domain, schema_id, extractors, dom_profile, trust_level,
		       success_rate, total_uses, total_repairs, contributors, created_at, updated_at
		FROM profiles WHERE domain = ?
		ORDER BY success_rate DESC, total_uses DESC`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProfiles(rows)
}

// ListProfiles returns all profiles, optionally filtered by trust level.
func (s *Store) ListProfiles(ctx context.Context, trustLevel string, limit int) ([]*Profile, error) {
	query := `SELECT id, url_pattern, domain, schema_id, extractors, dom_profile, trust_level,
	                 success_rate, total_uses, total_repairs, contributors, created_at, updated_at
	          FROM profiles`
	var args []any
	if trustLevel != "" {
		query += ` WHERE trust_level = ?`
		args = append(args, trustLevel)
	}
	query += ` ORDER BY success_rate DESC, total_uses DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProfiles(rows)
}

// UpdateProfile updates a profile's extractors and metadata.
func (s *Store) UpdateProfile(ctx context.Context, p *Profile) error {
	contribs, _ := json.Marshal(p.Contributors)
	p.UpdatedAt = time.Now().UnixMilli()

	_, err := s.DB.ExecContext(ctx, `
		UPDATE profiles SET
			url_pattern=?, domain=?, schema_id=?, extractors=?, dom_profile=?,
			trust_level=?, success_rate=?, total_uses=?, total_repairs=?,
			contributors=?, updated_at=?
		WHERE id=?`,
		p.URLPattern, p.Domain, p.SchemaID, p.Extractors, p.DOMProfile,
		p.TrustLevel, p.SuccessRate, p.TotalUses, p.TotalRepairs,
		string(contribs), p.UpdatedAt, p.ID,
	)
	return err
}

// DeleteProfile removes a profile by ID. Cascades to corrections and reports.
func (s *Store) DeleteProfile(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id)
	return err
}

// IncrementUses bumps total_uses for a profile.
func (s *Store) IncrementUses(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE profiles SET total_uses = total_uses + 1, updated_at = ?
		WHERE id = ?`, now, id)
	return err
}

// RecordSuccess adjusts success_rate upward using exponential moving average.
func (s *Store) RecordSuccess(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	// EMA with alpha=0.05: new_rate = old_rate * 0.95 + 1.0 * 0.05
	_, err := s.DB.ExecContext(ctx, `
		UPDATE profiles SET
			success_rate = MIN(1.0, success_rate * 0.95 + 0.05),
			updated_at = ?
		WHERE id = ?`, now, id)
	return err
}

// RecordFailure adjusts success_rate downward using exponential moving average.
func (s *Store) RecordFailure(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	// EMA with alpha=0.05: new_rate = old_rate * 0.95 + 0.0 * 0.05
	_, err := s.DB.ExecContext(ctx, `
		UPDATE profiles SET
			success_rate = MAX(0.0, success_rate * 0.95),
			total_repairs = total_repairs + 1,
			updated_at = ?
		WHERE id = ?`, now, id)
	return err
}

// AddContributor adds an instance ID to a profile's contributors list if not already present.
func (s *Store) AddContributor(ctx context.Context, profileID, instanceID string) error {
	p, err := s.GetProfile(ctx, profileID)
	if err != nil || p == nil {
		return err
	}
	for _, c := range p.Contributors {
		if c == instanceID {
			return nil // already present
		}
	}
	p.Contributors = append(p.Contributors, instanceID)
	contribs, _ := json.Marshal(p.Contributors)
	now := time.Now().UnixMilli()
	_, err = s.DB.ExecContext(ctx, `
		UPDATE profiles SET contributors = ?, updated_at = ?
		WHERE id = ?`, string(contribs), now, profileID)
	return err
}

// LeaderboardEntry holds leaderboard data for a domain.
type LeaderboardEntry struct {
	Domain       string  `json:"domain"`
	ProfileCount int     `json:"profile_count"`
	AvgSuccess   float64 `json:"avg_success_rate"`
	TotalUses    int     `json:"total_uses"`
	TotalRepairs int     `json:"total_repairs"`
	LastUpdated  int64   `json:"last_updated"`
}

// DomainLeaderboard returns aggregated stats per domain, ordered by success rate.
func (s *Store) DomainLeaderboard(ctx context.Context, limit int) ([]*LeaderboardEntry, error) {
	query := `
		SELECT domain, COUNT(*) as profile_count,
		       AVG(success_rate) as avg_success,
		       SUM(total_uses) as total_uses,
		       SUM(total_repairs) as total_repairs,
		       MAX(updated_at) as last_updated
		FROM profiles
		GROUP BY domain
		ORDER BY avg_success DESC, total_uses DESC`
	if limit > 0 {
		query += ` LIMIT ?`
	}

	var args []any
	if limit > 0 {
		args = append(args, limit)
	}

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*LeaderboardEntry
	for rows.Next() {
		e := &LeaderboardEntry{}
		if err := rows.Scan(&e.Domain, &e.ProfileCount, &e.AvgSuccess, &e.TotalUses, &e.TotalRepairs, &e.LastUpdated); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// CountProfiles returns the total number of profiles.
func (s *Store) CountProfiles(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM profiles`).Scan(&n)
	return n, err
}

func scanProfiles(rows *sql.Rows) ([]*Profile, error) {
	var profiles []*Profile
	for rows.Next() {
		p := &Profile{}
		var contribs string
		if err := rows.Scan(
			&p.ID, &p.URLPattern, &p.Domain, &p.SchemaID, &p.Extractors, &p.DOMProfile, &p.TrustLevel,
			&p.SuccessRate, &p.TotalUses, &p.TotalRepairs, &contribs, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(contribs), &p.Contributors)
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}
