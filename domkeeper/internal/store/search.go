package store

import (
	"context"
	"fmt"
	"strings"
)

// SearchResult is a chunk matched by full-text search.
type SearchResult struct {
	Chunk
	ContentTitle string  `json:"content_title"`
	PageURL      string  `json:"page_url"`
	TrustLevel   string  `json:"trust_level"`
	FolderID     string  `json:"folder_id,omitempty"`
	RuleName     string  `json:"rule_name"`
	Rank         float64 `json:"rank"`
}

// SearchOptions controls the FTS5 search behaviour.
type SearchOptions struct {
	Query      string   // FTS5 query string
	FolderIDs  []string // optional: restrict to these folders
	TrustLevel string   // optional: minimum trust level filter
	Limit      int      // max results (default: 20)
	Offset     int      // pagination offset
}

// Search performs a full-text search on chunks and returns ranked results.
func (s *Store) Search(ctx context.Context, opts SearchOptions) ([]*SearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}

	var where []string
	var args []any

	where = append(where, "chunks_fts MATCH ?")
	args = append(args, opts.Query)

	if len(opts.FolderIDs) > 0 {
		placeholders := make([]string, len(opts.FolderIDs))
		for i, fid := range opts.FolderIDs {
			placeholders[i] = "?"
			args = append(args, fid)
		}
		where = append(where, fmt.Sprintf("r.folder_id IN (%s)", strings.Join(placeholders, ",")))
	}

	if opts.TrustLevel != "" {
		where = append(where, "cc.trust_level = ?")
		args = append(args, opts.TrustLevel)
	}

	query := fmt.Sprintf(`
		SELECT c.id, c.content_id, c.chunk_index, c.text, c.token_count,
		       c.overlap_prev, c.metadata, c.created_at,
		       cc.title, cc.page_url, cc.trust_level,
		       COALESCE(r.folder_id, ''), r.name,
		       rank
		FROM chunks_fts
		JOIN chunks c ON c.rowid = chunks_fts.rowid
		JOIN content_cache cc ON cc.id = c.content_id
		JOIN extraction_rules r ON r.id = cc.rule_id
		WHERE %s
		ORDER BY rank
		LIMIT ? OFFSET ?`,
		strings.Join(where, " AND "),
	)
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*SearchResult
	for rows.Next() {
		sr := &SearchResult{}
		if err := rows.Scan(
			&sr.ID, &sr.ContentID, &sr.ChunkIndex, &sr.Text, &sr.TokenCount,
			&sr.OverlapPrev, &sr.Metadata, &sr.CreatedAt,
			&sr.ContentTitle, &sr.PageURL, &sr.TrustLevel,
			&sr.FolderID, &sr.RuleName,
			&sr.Rank,
		); err != nil {
			return nil, err
		}
		results = append(results, sr)
	}
	return results, rows.Err()
}

// CountChunks returns the total number of chunks in the store.
func (s *Store) CountChunks(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&n)
	return n, err
}

// CountContent returns the total number of content entries.
func (s *Store) CountContent(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_cache`).Scan(&n)
	return n, err
}
