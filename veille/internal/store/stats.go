package store

import "context"

// Stats returns aggregate counters for the shard.
func (s *Store) Stats(ctx context.Context) (*SpaceStats, error) {
	var stats SpaceStats
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sources`).Scan(&stats.Sources)
	if err != nil {
		return nil, err
	}
	err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM extractions`).Scan(&stats.Extractions)
	if err != nil {
		return nil, err
	}
	err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&stats.Chunks)
	if err != nil {
		return nil, err
	}
	err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fetch_log`).Scan(&stats.FetchLogs)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}
