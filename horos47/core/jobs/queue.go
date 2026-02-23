package jobs

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"horos47/core/data"
)

// Status représente l'état d'un job dans la queue
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusPoison     Status = "poison"
)

// Job représente une tâche asynchrone dans la queue SQLite
type Job struct {
	ID          data.UUID              `json:"id"`
	Type        string                 `json:"type"`
	Status      Status                 `json:"status"`
	Payload     map[string]interface{} `json:"payload"`
	Result      map[string]interface{} `json:"result,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Attempts    int                    `json:"attempts"`
	MaxAttempts int                    `json:"max_attempts"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
}

// Queue gère la file de jobs asynchrones via SQLite (pattern TriggerBox)
type Queue struct {
	db *sql.DB
}

// NewQueue crée une nouvelle queue et initialise le schéma
func NewQueue(db *sql.DB) (*Queue, error) {
	schema := `
		CREATE TABLE IF NOT EXISTS jobs (
			id BLOB PRIMARY KEY,
			type TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL,
			result TEXT,
			error TEXT,
			created_at INTEGER NOT NULL,
			claimed_at INTEGER,
			started_at INTEGER,
			completed_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
		CREATE INDEX IF NOT EXISTS idx_jobs_type_status ON jobs(type, status);
	`

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("failed to create jobs schema: %w", err)
	}

	// Idempotent migration: add attempts/max_attempts columns
	for _, col := range []struct{ name, def string }{
		{"attempts", "INTEGER DEFAULT 0"},
		{"max_attempts", "INTEGER DEFAULT 3"},
	} {
		_, _ = db.Exec(fmt.Sprintf("ALTER TABLE jobs ADD COLUMN %s %s", col.name, col.def))
	}

	return &Queue{db: db}, nil
}

// Submit ajoute un nouveau job dans la queue
func (q *Queue) Submit(jobType string, payload map[string]interface{}) (data.UUID, error) {
	id := data.NewUUID()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return data.UUID{}, fmt.Errorf("failed to marshal payload: %w", err)
	}

	_, err = data.ExecWithRetry(q.db, `
		INSERT INTO jobs (id, type, status, payload, created_at, attempts, max_attempts)
		VALUES (?, ?, ?, ?, ?, 0, 3)
	`, id, jobType, StatusPending, string(payloadJSON), time.Now().Unix())

	if err != nil {
		return data.UUID{}, fmt.Errorf("failed to insert job: %w", err)
	}

	return id, nil
}

// Poll récupère le prochain job pending et le marque processing (atomic)
func (q *Queue) Poll(jobType string) (*Job, error) {
	tx, err := q.db.Begin()
	if err != nil {
		return nil, err
	}
	defer data.SafeTxRollback(tx, "poll job")

	row := tx.QueryRow(`
		SELECT id, type, status, payload, COALESCE(attempts,0), COALESCE(max_attempts,3), created_at
		FROM jobs
		WHERE status = ? AND type = ?
		ORDER BY created_at ASC
		LIMIT 1
	`, StatusPending, jobType)

	var job Job
	var payloadJSON string
	var createdAtUnix int64

	if err := row.Scan(&job.ID, &job.Type, &job.Status, &payloadJSON, &job.Attempts, &job.MaxAttempts, &createdAtUnix); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if err := json.Unmarshal([]byte(payloadJSON), &job.Payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	job.CreatedAt = time.Unix(createdAtUnix, 0)

	now := time.Now()
	job.StartedAt = &now
	job.Status = StatusProcessing

	_, err = tx.Exec(`
		UPDATE jobs SET status = ?, started_at = ? WHERE id = ?
	`, StatusProcessing, now.Unix(), job.ID)

	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &job, nil
}

// rawJob holds raw DB row data before JSON parsing (to release tx faster)
type rawJob struct {
	id           data.UUID
	jobType      string
	status       Status
	payloadJSON  string
	attempts     int
	maxAttempts  int
	createdAtUnix int64
}

// PollBatch récupère N jobs pending et les marque processing atomiquement.
// Transaction is kept minimal: UPDATE + SELECT raw rows + Commit.
// JSON parsing happens after commit to avoid holding the write lock.
func (q *Queue) PollBatch(jobType string, limit int) ([]*Job, error) {
	now := time.Now()

	// Minimal transaction: claim + read raw rows
	var rawJobs []rawJob
	err := data.RunTransaction(q.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE jobs SET status = ?, started_at = ?
			WHERE id IN (
				SELECT id FROM jobs
				WHERE status = ? AND type = ?
				ORDER BY created_at ASC
				LIMIT ?
			)
		`, StatusProcessing, now.Unix(), StatusPending, jobType, limit)
		if err != nil {
			return err
		}

		rows, err := tx.Query(`
			SELECT id, type, status, payload, COALESCE(attempts,0), COALESCE(max_attempts,3), created_at
			FROM jobs
			WHERE status = ? AND type = ? AND started_at = ?
			ORDER BY created_at ASC
		`, StatusProcessing, jobType, now.Unix())
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var rj rawJob
			if err := rows.Scan(&rj.id, &rj.jobType, &rj.status, &rj.payloadJSON, &rj.attempts, &rj.maxAttempts, &rj.createdAtUnix); err != nil {
				return err
			}
			rawJobs = append(rawJobs, rj)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	// JSON parsing after tx commit — no write lock held
	jobs := make([]*Job, 0, len(rawJobs))
	for _, rj := range rawJobs {
		job := &Job{
			ID:          rj.id,
			Type:        rj.jobType,
			Status:      rj.status,
			Attempts:    rj.attempts,
			MaxAttempts: rj.maxAttempts,
			CreatedAt:   time.Unix(rj.createdAtUnix, 0),
		}
		t := now
		job.StartedAt = &t

		if err := json.Unmarshal([]byte(rj.payloadJSON), &job.Payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// Complete marque un job completed avec résultat
func (q *Queue) Complete(jobID data.UUID, result map[string]interface{}) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	now := time.Now()
	_, err = data.ExecWithRetry(q.db, `
		UPDATE jobs SET status = ?, result = ?, completed_at = ? WHERE id = ?
	`, StatusCompleted, string(resultJSON), now.Unix(), jobID)

	return err
}

// Fail marque un job failed avec erreur, incrémente attempts.
// Si attempts >= max_attempts, le job passe en poison.
func (q *Queue) Fail(jobID data.UUID, errMsg string) error {
	now := time.Now()

	_, err := data.ExecWithRetry(q.db, `
		UPDATE jobs SET
			status = CASE
				WHEN COALESCE(attempts,0) + 1 >= COALESCE(max_attempts,3) THEN 'poison'
				ELSE 'failed'
			END,
			error = ?,
			attempts = COALESCE(attempts,0) + 1,
			completed_at = ?
		WHERE id = ?
	`, errMsg, now.Unix(), jobID)

	return err
}

// RetryFailed remet en pending les jobs failed dont attempts < max_attempts.
// Les jobs poison (attempts >= max_attempts) restent bloqués.
// Returns number of jobs retried.
func (q *Queue) RetryFailed() (int64, error) {
	result, err := data.ExecWithRetry(q.db, `
		UPDATE jobs SET status = 'pending', started_at = NULL, completed_at = NULL
		WHERE status = 'failed' AND COALESCE(attempts,0) < COALESCE(max_attempts,3)
	`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Get récupère un job par ID
func (q *Queue) Get(jobID data.UUID) (*Job, error) {
	row := q.db.QueryRow(`
		SELECT id, type, status, payload, result, error,
			COALESCE(attempts,0), COALESCE(max_attempts,3),
			created_at, started_at, completed_at
		FROM jobs WHERE id = ?
	`, jobID)

	var job Job
	var payloadJSON, resultJSON sql.NullString
	var createdAtUnix int64
	var startedAtUnix, completedAtUnix sql.NullInt64

	err := row.Scan(
		&job.ID, &job.Type, &job.Status,
		&payloadJSON, &resultJSON, &job.Error,
		&job.Attempts, &job.MaxAttempts,
		&createdAtUnix, &startedAtUnix, &completedAtUnix,
	)

	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(payloadJSON.String), &job.Payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	if resultJSON.Valid && resultJSON.String != "" {
		if err := json.Unmarshal([]byte(resultJSON.String), &job.Result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal result: %w", err)
		}
	}

	job.CreatedAt = time.Unix(createdAtUnix, 0)

	if startedAtUnix.Valid {
		t := time.Unix(startedAtUnix.Int64, 0)
		job.StartedAt = &t
	}

	if completedAtUnix.Valid {
		t := time.Unix(completedAtUnix.Int64, 0)
		job.CompletedAt = &t
	}

	return &job, nil
}
