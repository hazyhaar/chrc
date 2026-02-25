// CLAUDE:SUMMARY SQLite database handle for domkeeper — opens DB with HOROS pragmas and applies schema.
// Package store provides the SQLite persistence layer for domkeeper.
package store

import (
	"database/sql"

	"github.com/hazyhaar/pkg/dbopen"
)

// Store is the domkeeper database handle.
type Store struct {
	DB *sql.DB
}

// Open opens (or creates) the domkeeper SQLite database at path,
// applies HOROS pragmas and the domkeeper schema.
func Open(path string, opts ...dbopen.Option) (*Store, error) {
	allOpts := append([]dbopen.Option{
		dbopen.WithMkdirAll(),
		dbopen.WithSchema(Schema),
	}, opts...)

	db, err := dbopen.Open(path, allOpts...)
	if err != nil {
		return nil, err
	}
	return &Store{DB: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.DB.Close()
}
