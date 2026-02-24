// Package store provides the data access layer for veille shards.
//
// Unlike domkeeper which opens its own database, veille receives a *sql.DB
// from the usertenant pool. Each store instance is bound to one user×space shard.
package store

import "database/sql"

// Store wraps a shard database for veille operations.
type Store struct {
	DB *sql.DB
}

// NewStore creates a Store from an already-opened database connection.
func NewStore(db *sql.DB) *Store {
	return &Store{DB: db}
}
