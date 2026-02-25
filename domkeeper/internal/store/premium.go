// CLAUDE:SUMMARY Search tier analytics — logs free/premium search requests and provides aggregate stats.
package store

import (
	"context"
	"time"
)

// SearchTierLog records a search request for analytics.
type SearchTierLog struct {
	ID           string `json:"id"`
	UserID       string `json:"user_id"`
	Tier         string `json:"tier"` // "free" or "premium"
	Query        string `json:"query"`
	Passes       int    `json:"passes"`
	ResultsCount int    `json:"results_count"`
	LatencyMs    int64  `json:"latency_ms"`
	CreatedAt    int64  `json:"created_at"`
}

// InsertSearchTierLog records a search request.
func (s *Store) InsertSearchTierLog(ctx context.Context, l *SearchTierLog) error {
	now := time.Now().UnixMilli()
	if l.CreatedAt == 0 {
		l.CreatedAt = now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO search_tiers (id, user_id, tier, query, passes, results_count, latency_ms, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		l.ID, l.UserID, l.Tier, l.Query, l.Passes, l.ResultsCount, l.LatencyMs, l.CreatedAt,
	)
	return err
}

// SearchTierStats holds aggregate search tier statistics.
type SearchTierStats struct {
	Tier          string  `json:"tier"`
	TotalQueries  int     `json:"total_queries"`
	AvgPasses     float64 `json:"avg_passes"`
	AvgResults    float64 `json:"avg_results"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	TotalResults  int     `json:"total_results"`
}

// GetSearchTierStats returns aggregate stats grouped by tier.
func (s *Store) GetSearchTierStats(ctx context.Context) ([]*SearchTierStats, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT tier, COUNT(*) as total_queries,
		       AVG(passes) as avg_passes,
		       AVG(results_count) as avg_results,
		       AVG(latency_ms) as avg_latency_ms,
		       SUM(results_count) as total_results
		FROM search_tiers
		GROUP BY tier
		ORDER BY tier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []*SearchTierStats
	for rows.Next() {
		s := &SearchTierStats{}
		if err := rows.Scan(&s.Tier, &s.TotalQueries, &s.AvgPasses, &s.AvgResults, &s.AvgLatencyMs, &s.TotalResults); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// RecentSearchTierLogs returns the most recent search tier log entries.
func (s *Store) RecentSearchTierLogs(ctx context.Context, limit int) ([]*SearchTierLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, tier, query, passes, results_count, latency_ms, created_at
		FROM search_tiers
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*SearchTierLog
	for rows.Next() {
		l := &SearchTierLog{}
		if err := rows.Scan(&l.ID, &l.UserID, &l.Tier, &l.Query, &l.Passes, &l.ResultsCount, &l.LatencyMs, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}
