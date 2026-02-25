// CLAUDE:SUMMARY Tracked question CRUD, scheduling, and auto-source creation.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// InsertQuestion adds a tracked question to the shard.
func (s *Store) InsertQuestion(ctx context.Context, q *TrackedQuestion) error {
	now := time.Now().UnixMilli()
	if q.CreatedAt == 0 {
		q.CreatedAt = now
	}
	if q.UpdatedAt == 0 {
		q.UpdatedAt = now
	}
	if q.Channels == "" {
		q.Channels = "[]"
	}
	if q.ScheduleMs == 0 {
		q.ScheduleMs = 86400000
	}
	if q.MaxResults == 0 {
		q.MaxResults = 20
	}

	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO tracked_questions (id, text, keywords, channels, schedule_ms,
		max_results, follow_links, enabled, last_run_at, last_result_count,
		total_results, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.ID, q.Text, q.Keywords, q.Channels, q.ScheduleMs,
		q.MaxResults, q.FollowLinks, q.Enabled, q.LastRunAt,
		q.LastResultCount, q.TotalResults, q.CreatedAt, q.UpdatedAt,
	)
	return err
}

// GetQuestion retrieves a tracked question by ID.
func (s *Store) GetQuestion(ctx context.Context, id string) (*TrackedQuestion, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, text, keywords, channels, schedule_ms, max_results,
		follow_links, enabled, last_run_at, last_result_count, total_results,
		created_at, updated_at
		FROM tracked_questions WHERE id = ?`, id)
	return scanQuestion(row)
}

// ListQuestions returns all tracked questions in the shard.
func (s *Store) ListQuestions(ctx context.Context) ([]*TrackedQuestion, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, text, keywords, channels, schedule_ms, max_results,
		follow_links, enabled, last_run_at, last_result_count, total_results,
		created_at, updated_at
		FROM tracked_questions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var questions []*TrackedQuestion
	for rows.Next() {
		q, err := scanQuestionRows(rows)
		if err != nil {
			return nil, err
		}
		questions = append(questions, q)
	}
	return questions, rows.Err()
}

// UpdateQuestion updates a tracked question's mutable fields.
func (s *Store) UpdateQuestion(ctx context.Context, q *TrackedQuestion) error {
	q.UpdatedAt = time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx,
		`UPDATE tracked_questions SET text=?, keywords=?, channels=?,
		schedule_ms=?, max_results=?, follow_links=?, enabled=?, updated_at=?
		WHERE id=?`,
		q.Text, q.Keywords, q.Channels, q.ScheduleMs,
		q.MaxResults, q.FollowLinks, q.Enabled, q.UpdatedAt, q.ID,
	)
	return err
}

// DeleteQuestion removes a tracked question by ID.
func (s *Store) DeleteQuestion(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM tracked_questions WHERE id = ?`, id)
	return err
}

// DueQuestions returns enabled questions whose next run time has passed.
// next run = last_run_at + schedule_ms
// Questions with nil last_run_at are always due.
func (s *Store) DueQuestions(ctx context.Context) ([]*TrackedQuestion, error) {
	now := time.Now().UnixMilli()
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, text, keywords, channels, schedule_ms, max_results,
		follow_links, enabled, last_run_at, last_result_count, total_results,
		created_at, updated_at
		FROM tracked_questions
		WHERE enabled = 1
		  AND (last_run_at IS NULL OR last_run_at + schedule_ms <= ?)
		ORDER BY last_run_at ASC NULLS FIRST`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var questions []*TrackedQuestion
	for rows.Next() {
		q, err := scanQuestionRows(rows)
		if err != nil {
			return nil, err
		}
		questions = append(questions, q)
	}
	return questions, rows.Err()
}

// RecordQuestionRun updates a question after a successful run.
func (s *Store) RecordQuestionRun(ctx context.Context, id string, newCount int) error {
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx,
		`UPDATE tracked_questions SET last_run_at=?, last_result_count=?,
		total_results=total_results+?, updated_at=?
		WHERE id=?`, now, newCount, newCount, now, id)
	return err
}

func scanQuestion(row *sql.Row) (*TrackedQuestion, error) {
	var q TrackedQuestion
	var enabled, followLinks int
	err := row.Scan(
		&q.ID, &q.Text, &q.Keywords, &q.Channels, &q.ScheduleMs,
		&q.MaxResults, &followLinks, &enabled, &q.LastRunAt,
		&q.LastResultCount, &q.TotalResults, &q.CreatedAt, &q.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan question: %w", err)
	}
	q.Enabled = enabled != 0
	q.FollowLinks = followLinks != 0
	return &q, nil
}

func scanQuestionRows(rows *sql.Rows) (*TrackedQuestion, error) {
	var q TrackedQuestion
	var enabled, followLinks int
	err := rows.Scan(
		&q.ID, &q.Text, &q.Keywords, &q.Channels, &q.ScheduleMs,
		&q.MaxResults, &followLinks, &enabled, &q.LastRunAt,
		&q.LastResultCount, &q.TotalResults, &q.CreatedAt, &q.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan question: %w", err)
	}
	q.Enabled = enabled != 0
	q.FollowLinks = followLinks != 0
	return &q, nil
}
