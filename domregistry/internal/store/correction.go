package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Correction is a proposed change to a profile's extractors.
type Correction struct {
	ID            string `json:"id"`
	ProfileID     string `json:"profile_id"`
	InstanceID    string `json:"instance_id"`
	OldExtractors string `json:"old_extractors"`
	NewExtractors string `json:"new_extractors"`
	Reason        string `json:"reason"` // "layout_change", "new_field", "selector_broken"
	Validated     int    `json:"validated"`
	CreatedAt     int64  `json:"created_at"`
}

// Report is a failure signal from an instance about a profile.
type Report struct {
	ID         string `json:"id"`
	ProfileID  string `json:"profile_id"`
	InstanceID string `json:"instance_id"`
	ErrorType  string `json:"error_type"`
	Message    string `json:"message"`
	CreatedAt  int64  `json:"created_at"`
}

// InstanceReputation tracks contribution quality per instance.
type InstanceReputation struct {
	InstanceID          string `json:"instance_id"`
	CorrectionsAccepted int    `json:"corrections_accepted"`
	CorrectionsRejected int    `json:"corrections_rejected"`
	CorrectionsPending  int    `json:"corrections_pending"`
	DomainsCovered      int    `json:"domains_covered"`
	LastActive          int64  `json:"last_active"`
	CreatedAt           int64  `json:"created_at"`
}

// InsertCorrection inserts a new correction proposal.
func (s *Store) InsertCorrection(ctx context.Context, c *Correction) error {
	now := time.Now().UnixMilli()
	if c.CreatedAt == 0 {
		c.CreatedAt = now
	}

	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO corrections (id, profile_id, instance_id, old_extractors, new_extractors, reason, validated, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		c.ID, c.ProfileID, c.InstanceID, c.OldExtractors, c.NewExtractors, c.Reason, c.Validated, c.CreatedAt,
	)
	if err != nil {
		return err
	}

	// Update instance reputation: increment pending
	return s.ensureReputation(ctx, c.InstanceID, func(r *InstanceReputation) {
		r.CorrectionsPending++
	})
}

// GetCorrection retrieves a correction by ID.
func (s *Store) GetCorrection(ctx context.Context, id string) (*Correction, error) {
	c := &Correction{}
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, profile_id, instance_id, old_extractors, new_extractors, reason, validated, created_at
		FROM corrections WHERE id = ?`, id).Scan(
		&c.ID, &c.ProfileID, &c.InstanceID, &c.OldExtractors, &c.NewExtractors, &c.Reason, &c.Validated, &c.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ListCorrectionsByProfile returns corrections for a profile, optionally filtered by status.
func (s *Store) ListCorrectionsByProfile(ctx context.Context, profileID string, status *int) ([]*Correction, error) {
	query := `SELECT id, profile_id, instance_id, old_extractors, new_extractors, reason, validated, created_at
	          FROM corrections WHERE profile_id = ?`
	var args []any
	args = append(args, profileID)
	if status != nil {
		query += ` AND validated = ?`
		args = append(args, *status)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCorrections(rows)
}

// ListPendingCorrections returns all pending corrections.
func (s *Store) ListPendingCorrections(ctx context.Context, limit int) ([]*Correction, error) {
	query := `SELECT id, profile_id, instance_id, old_extractors, new_extractors, reason, validated, created_at
	          FROM corrections WHERE validated = 0 ORDER BY created_at ASC`
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
	return scanCorrections(rows)
}

// AcceptCorrection marks a correction as accepted (validated=1) and applies it to the profile.
func (s *Store) AcceptCorrection(ctx context.Context, correctionID string) error {
	c, err := s.GetCorrection(ctx, correctionID)
	if err != nil || c == nil {
		return err
	}
	if c.Validated != 0 {
		return nil // already resolved
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UnixMilli()

	// Mark correction accepted
	if _, err := tx.ExecContext(ctx, `UPDATE corrections SET validated = 1 WHERE id = ?`, correctionID); err != nil {
		return err
	}

	// Apply new extractors to the profile
	if _, err := tx.ExecContext(ctx, `
		UPDATE profiles SET extractors = ?, total_repairs = total_repairs + 1, updated_at = ?
		WHERE id = ?`, c.NewExtractors, now, c.ProfileID); err != nil {
		return err
	}

	// Update instance reputation
	if _, err := tx.ExecContext(ctx, `
		UPDATE instance_reputation SET
			corrections_accepted = corrections_accepted + 1,
			corrections_pending = MAX(0, corrections_pending - 1),
			last_active = ?
		WHERE instance_id = ?`, now, c.InstanceID); err != nil {
		return err
	}

	return tx.Commit()
}

// RejectCorrection marks a correction as rejected (validated=-1).
func (s *Store) RejectCorrection(ctx context.Context, correctionID string) error {
	c, err := s.GetCorrection(ctx, correctionID)
	if err != nil || c == nil {
		return err
	}
	if c.Validated != 0 {
		return nil // already resolved
	}

	now := time.Now().UnixMilli()

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE corrections SET validated = -1 WHERE id = ?`, correctionID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE instance_reputation SET
			corrections_rejected = corrections_rejected + 1,
			corrections_pending = MAX(0, corrections_pending - 1),
			last_active = ?
		WHERE instance_id = ?`, now, c.InstanceID); err != nil {
		return err
	}

	return tx.Commit()
}

// ScoreCorrection evaluates whether a correction should be auto-accepted.
// Returns true if the correction should be accepted automatically.
//
// Auto-accept criteria:
//   - Instance has >= 3 accepted corrections AND < 20% rejection rate
//   - OR trust_level of profile is "community" (lower bar)
func (s *Store) ScoreCorrection(ctx context.Context, correctionID string) (bool, error) {
	c, err := s.GetCorrection(ctx, correctionID)
	if err != nil || c == nil {
		return false, err
	}

	rep, err := s.GetReputation(ctx, c.InstanceID)
	if err != nil {
		return false, err
	}

	// New instance with no history: never auto-accept
	if rep == nil || rep.CorrectionsAccepted == 0 {
		return false, nil
	}

	total := rep.CorrectionsAccepted + rep.CorrectionsRejected
	if total == 0 {
		return false, nil
	}

	rejectionRate := float64(rep.CorrectionsRejected) / float64(total)

	// Auto-accept: >= 3 accepted corrections AND < 20% rejection rate
	if rep.CorrectionsAccepted >= 3 && rejectionRate < 0.2 {
		return true, nil
	}

	return false, nil
}

// InsertReport inserts a failure report and adjusts the profile's success_rate.
func (s *Store) InsertReport(ctx context.Context, r *Report) error {
	now := time.Now().UnixMilli()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reports (id, profile_id, instance_id, error_type, message, created_at)
		VALUES (?,?,?,?,?,?)`,
		r.ID, r.ProfileID, r.InstanceID, r.ErrorType, r.Message, r.CreatedAt,
	); err != nil {
		return err
	}

	// Decrement success_rate via EMA
	if _, err := tx.ExecContext(ctx, `
		UPDATE profiles SET success_rate = MAX(0.0, success_rate * 0.95), updated_at = ?
		WHERE id = ?`, now, r.ProfileID); err != nil {
		return err
	}

	return tx.Commit()
}

// GetReputation retrieves an instance's reputation.
func (s *Store) GetReputation(ctx context.Context, instanceID string) (*InstanceReputation, error) {
	r := &InstanceReputation{}
	err := s.DB.QueryRowContext(ctx, `
		SELECT instance_id, corrections_accepted, corrections_rejected, corrections_pending,
		       domains_covered, last_active, created_at
		FROM instance_reputation WHERE instance_id = ?`, instanceID).Scan(
		&r.InstanceID, &r.CorrectionsAccepted, &r.CorrectionsRejected, &r.CorrectionsPending,
		&r.DomainsCovered, &r.LastActive, &r.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// InstanceLeaderboard returns instance reputations ordered by accepted corrections.
func (s *Store) InstanceLeaderboard(ctx context.Context, limit int) ([]*InstanceReputation, error) {
	query := `SELECT instance_id, corrections_accepted, corrections_rejected, corrections_pending,
	                 domains_covered, last_active, created_at
	          FROM instance_reputation
	          ORDER BY corrections_accepted DESC, corrections_rejected ASC`
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

	var reps []*InstanceReputation
	for rows.Next() {
		r := &InstanceReputation{}
		if err := rows.Scan(
			&r.InstanceID, &r.CorrectionsAccepted, &r.CorrectionsRejected, &r.CorrectionsPending,
			&r.DomainsCovered, &r.LastActive, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		reps = append(reps, r)
	}
	return reps, rows.Err()
}

// CountCorrections returns the total number of corrections.
func (s *Store) CountCorrections(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM corrections`).Scan(&n)
	return n, err
}

// CountReports returns the total number of reports.
func (s *Store) CountReports(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reports`).Scan(&n)
	return n, err
}

// ensureReputation creates or updates an instance reputation row.
func (s *Store) ensureReputation(ctx context.Context, instanceID string, mutate func(*InstanceReputation)) error {
	now := time.Now().UnixMilli()
	rep, err := s.GetReputation(ctx, instanceID)
	if err != nil {
		return err
	}

	if rep == nil {
		rep = &InstanceReputation{
			InstanceID: instanceID,
			LastActive: now,
			CreatedAt:  now,
		}
		mutate(rep)
		_, err := s.DB.ExecContext(ctx, `
			INSERT INTO instance_reputation
				(instance_id, corrections_accepted, corrections_rejected, corrections_pending,
				 domains_covered, last_active, created_at)
			VALUES (?,?,?,?,?,?,?)`,
			rep.InstanceID, rep.CorrectionsAccepted, rep.CorrectionsRejected, rep.CorrectionsPending,
			rep.DomainsCovered, rep.LastActive, rep.CreatedAt,
		)
		return err
	}

	mutate(rep)
	rep.LastActive = now
	_, err = s.DB.ExecContext(ctx, `
		UPDATE instance_reputation SET
			corrections_accepted=?, corrections_rejected=?, corrections_pending=?,
			domains_covered=?, last_active=?
		WHERE instance_id=?`,
		rep.CorrectionsAccepted, rep.CorrectionsRejected, rep.CorrectionsPending,
		rep.DomainsCovered, rep.LastActive, rep.InstanceID,
	)
	return err
}

func scanCorrections(rows *sql.Rows) ([]*Correction, error) {
	var corrections []*Correction
	for rows.Next() {
		c := &Correction{}
		if err := rows.Scan(
			&c.ID, &c.ProfileID, &c.InstanceID, &c.OldExtractors, &c.NewExtractors, &c.Reason, &c.Validated, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		corrections = append(corrections, c)
	}
	return corrections, rows.Err()
}
