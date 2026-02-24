package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// GPUPricing represents a GPU provider's pricing and performance metrics.
type GPUPricing struct {
	ID           string  `json:"id"`
	Provider     string  `json:"provider"`     // runpod, vast, lambdalabs
	GPUModel     string  `json:"gpu_model"`    // h100, a100, l40s
	CostPerSec   float64 `json:"cost_per_sec"` // real cost per second
	MinCommitHrs float64 `json:"min_commit_hrs"`
	Throughput   float64 `json:"throughput"`    // units/sec measured
	CostPerUnit  float64 `json:"cost_per_unit"` // derived: cost_per_sec / throughput
	IsDedicated  bool    `json:"is_dedicated"`
	Status       string  `json:"status"` // "active", "paused", "retired"
	MeasuredAt   int64   `json:"measured_at"`
}

// GPUThreshold represents the computed serverless vs dedicated decision.
type GPUThreshold struct {
	ID             string  `json:"id"`
	BacklogUnits   int     `json:"backlog_units"`
	ServerlessCost float64 `json:"serverless_cost"`
	DedicatedCost  float64 `json:"dedicated_cost"`
	Decision       string  `json:"decision"` // "serverless", "dedicated", "alert"
	ComputedAt     int64   `json:"computed_at"`
}

// InsertGPUPricing inserts a new GPU pricing entry.
func (s *Store) InsertGPUPricing(ctx context.Context, g *GPUPricing) error {
	if g.MeasuredAt == 0 {
		g.MeasuredAt = time.Now().UnixMilli()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO gpu_pricing
			(id, provider, gpu_model, cost_per_sec, min_commit_hrs, throughput, cost_per_unit,
			 is_dedicated, status, measured_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.Provider, g.GPUModel, g.CostPerSec, g.MinCommitHrs, g.Throughput, g.CostPerUnit,
		boolInt(g.IsDedicated), g.Status, g.MeasuredAt,
	)
	return err
}

// GetGPUPricing retrieves a GPU pricing entry by ID.
func (s *Store) GetGPUPricing(ctx context.Context, id string) (*GPUPricing, error) {
	g := &GPUPricing{}
	var isDedicated int
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, provider, gpu_model, cost_per_sec, min_commit_hrs, throughput, cost_per_unit,
		       is_dedicated, status, measured_at
		FROM gpu_pricing WHERE id = ?`, id).Scan(
		&g.ID, &g.Provider, &g.GPUModel, &g.CostPerSec, &g.MinCommitHrs, &g.Throughput, &g.CostPerUnit,
		&isDedicated, &g.Status, &g.MeasuredAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.IsDedicated = isDedicated != 0
	return g, nil
}

// ListGPUPricing returns all GPU pricing entries, optionally filtered by status.
func (s *Store) ListGPUPricing(ctx context.Context, activeOnly bool) ([]*GPUPricing, error) {
	query := `SELECT id, provider, gpu_model, cost_per_sec, min_commit_hrs, throughput, cost_per_unit,
	                 is_dedicated, status, measured_at
	          FROM gpu_pricing`
	if activeOnly {
		query += ` WHERE status = 'active'`
	}
	query += ` ORDER BY cost_per_unit ASC`

	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*GPUPricing
	for rows.Next() {
		g := &GPUPricing{}
		var isDedicated int
		if err := rows.Scan(
			&g.ID, &g.Provider, &g.GPUModel, &g.CostPerSec, &g.MinCommitHrs, &g.Throughput, &g.CostPerUnit,
			&isDedicated, &g.Status, &g.MeasuredAt,
		); err != nil {
			return nil, err
		}
		g.IsDedicated = isDedicated != 0
		entries = append(entries, g)
	}
	return entries, rows.Err()
}

// UpdateGPUPricing updates a GPU pricing entry.
func (s *Store) UpdateGPUPricing(ctx context.Context, g *GPUPricing) error {
	g.MeasuredAt = time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE gpu_pricing SET
			provider=?, gpu_model=?, cost_per_sec=?, min_commit_hrs=?, throughput=?, cost_per_unit=?,
			is_dedicated=?, status=?, measured_at=?
		WHERE id=?`,
		g.Provider, g.GPUModel, g.CostPerSec, g.MinCommitHrs, g.Throughput, g.CostPerUnit,
		boolInt(g.IsDedicated), g.Status, g.MeasuredAt, g.ID,
	)
	return err
}

// DeleteGPUPricing removes a GPU pricing entry.
func (s *Store) DeleteGPUPricing(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM gpu_pricing WHERE id = ?`, id)
	return err
}

// CheapestServerless returns the cheapest active serverless GPU pricing.
func (s *Store) CheapestServerless(ctx context.Context) (*GPUPricing, error) {
	g := &GPUPricing{}
	var isDedicated int
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, provider, gpu_model, cost_per_sec, min_commit_hrs, throughput, cost_per_unit,
		       is_dedicated, status, measured_at
		FROM gpu_pricing
		WHERE status = 'active' AND is_dedicated = 0
		ORDER BY cost_per_unit ASC LIMIT 1`).Scan(
		&g.ID, &g.Provider, &g.GPUModel, &g.CostPerSec, &g.MinCommitHrs, &g.Throughput, &g.CostPerUnit,
		&isDedicated, &g.Status, &g.MeasuredAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.IsDedicated = isDedicated != 0
	return g, nil
}

// CheapestDedicated returns the cheapest active dedicated GPU pricing.
func (s *Store) CheapestDedicated(ctx context.Context) (*GPUPricing, error) {
	g := &GPUPricing{}
	var isDedicated int
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, provider, gpu_model, cost_per_sec, min_commit_hrs, throughput, cost_per_unit,
		       is_dedicated, status, measured_at
		FROM gpu_pricing
		WHERE status = 'active' AND is_dedicated = 1
		ORDER BY cost_per_unit ASC LIMIT 1`).Scan(
		&g.ID, &g.Provider, &g.GPUModel, &g.CostPerSec, &g.MinCommitHrs, &g.Throughput, &g.CostPerUnit,
		&isDedicated, &g.Status, &g.MeasuredAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.IsDedicated = isDedicated != 0
	return g, nil
}

// UpsertGPUThreshold inserts or updates the GPU threshold computation.
func (s *Store) UpsertGPUThreshold(ctx context.Context, t *GPUThreshold) error {
	if t.ID == "" {
		t.ID = "default"
	}
	if t.ComputedAt == 0 {
		t.ComputedAt = time.Now().UnixMilli()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO gpu_threshold (id, backlog_units, serverless_cost, dedicated_cost, decision, computed_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			backlog_units=excluded.backlog_units,
			serverless_cost=excluded.serverless_cost,
			dedicated_cost=excluded.dedicated_cost,
			decision=excluded.decision,
			computed_at=excluded.computed_at`,
		t.ID, t.BacklogUnits, t.ServerlessCost, t.DedicatedCost, t.Decision, t.ComputedAt,
	)
	return err
}

// GetGPUThreshold retrieves the current GPU threshold.
func (s *Store) GetGPUThreshold(ctx context.Context) (*GPUThreshold, error) {
	t := &GPUThreshold{}
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, backlog_units, serverless_cost, dedicated_cost, decision, computed_at
		FROM gpu_threshold WHERE id = 'default'`).Scan(
		&t.ID, &t.BacklogUnits, &t.ServerlessCost, &t.DedicatedCost, &t.Decision, &t.ComputedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// ComputeGPUThreshold calculates the serverless vs dedicated decision based on
// the current backlog and pricing. Returns the updated threshold.
//
// Logic: if projected monthly serverless cost exceeds 80% of monthly dedicated cost,
// the decision is "alert" (human should provision dedicated).
func (s *Store) ComputeGPUThreshold(ctx context.Context, backlogUnits int) (*GPUThreshold, error) {
	serverless, err := s.CheapestServerless(ctx)
	if err != nil {
		return nil, err
	}
	dedicated, err := s.CheapestDedicated(ctx)
	if err != nil {
		return nil, err
	}

	var serverlessCost, dedicatedCost float64
	decision := "serverless"

	if serverless != nil && serverless.CostPerUnit > 0 {
		serverlessCost = float64(backlogUnits) * serverless.CostPerUnit
	}
	if dedicated != nil && dedicated.CostPerSec > 0 {
		// Monthly cost: cost_per_sec * 3600 * 24 * 30
		dedicatedCost = dedicated.CostPerSec * 2592000
	}

	// If monthly serverless projection > 80% of dedicated, alert
	if dedicatedCost > 0 && serverlessCost > dedicatedCost*0.8 {
		decision = "alert"
	}
	// If already on dedicated
	if dedicated != nil && dedicated.IsDedicated {
		decision = "dedicated"
	}

	t := &GPUThreshold{
		ID:             "default",
		BacklogUnits:   backlogUnits,
		ServerlessCost: serverlessCost,
		DedicatedCost:  dedicatedCost,
		Decision:       decision,
		ComputedAt:     time.Now().UnixMilli(),
	}

	if err := s.UpsertGPUThreshold(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}
