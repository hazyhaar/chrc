package data

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// OpenDB ouvre connexion SQLite avec configuration standard
func OpenDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configuration standard HOROS47
	pragmas := []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=10000",
		"PRAGMA synchronous=NORMAL",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma: %w", err)
		}
	}

	// Test connexion
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// RunTransaction exécute callback dans transaction avec retry automatique
func RunTransaction(db *sql.DB, fn func(*sql.Tx) error) error {
	maxRetries := 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		tx, err := db.Begin()
		if err != nil {
			if attempt < maxRetries-1 {
				continue
			}
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		err = fn(tx)
		if err != nil {
			tx.Rollback()
			if attempt < maxRetries-1 && isBusyError(err) {
				continue
			}
			return err
		}

		err = tx.Commit()
		if err != nil {
			if attempt < maxRetries-1 && isBusyError(err) {
				continue
			}
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		return nil
	}

	return fmt.Errorf("transaction failed after %d retries", maxRetries)
}

// ExecWithRetry exécute statement avec retry automatique si SQLITE_BUSY
func ExecWithRetry(db *sql.DB, query string, args ...interface{}) (sql.Result, error) {
	maxRetries := 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		result, err := db.Exec(query, args...)
		if err != nil {
			if attempt < maxRetries-1 && isBusyError(err) {
				continue
			}
			return nil, err
		}
		return result, nil
	}

	return nil, fmt.Errorf("exec failed after %d retries", maxRetries)
}

// QueryWithRetry exécute query avec retry automatique si SQLITE_BUSY
func QueryWithRetry(db *sql.DB, query string, args ...interface{}) (*sql.Rows, error) {
	maxRetries := 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		rows, err := db.Query(query, args...)
		if err != nil {
			if attempt < maxRetries-1 && isBusyError(err) {
				continue
			}
			return nil, err
		}
		return rows, nil
	}

	return nil, fmt.Errorf("query failed after %d retries", maxRetries)
}

// isBusyError vérifie si erreur est SQLITE_BUSY
func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	// Simplification: en production, vérifier code erreur SQLite exact
	return err.Error() == "database is locked"
}
