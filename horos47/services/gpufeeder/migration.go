package gpufeeder

import (
	"database/sql"
	"strings"
)

// MigrateSchema applique les migrations incrémentales au schéma gpu_jobs.
// Conçu pour être idempotent : les erreurs "duplicate column name" sont ignorées.
func MigrateSchema(db *sql.DB) error {
	// Migration 1 : colonne prompt_hash pour déduplication sémantique Think
	_, err := db.Exec(`ALTER TABLE gpu_jobs ADD COLUMN prompt_hash TEXT`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}

	// Index unique partiel : un seul job actif par prompt_hash
	_, err = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_gpu_jobs_prompt_hash
		ON gpu_jobs(prompt_hash)
		WHERE prompt_hash IS NOT NULL AND status IN ('processing', 'done')
	`)
	if err != nil {
		return err
	}

	return nil
}
