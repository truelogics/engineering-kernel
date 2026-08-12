// Package sqlite implements kernel.Storage against SQLite — v1's only
// Storage backend. See ARCHITECTURE.md, DATABASE.md.
package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// Store is a SQLite-backed kernel.Storage.
type Store struct {
	db *sql.DB
}

var _ kernel.Storage = (*Store)(nil)

// Open opens (creating if necessary) a SQLite database at path and
// applies the schema. Pass ":memory:" for an ephemeral, test-only store.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}
	// v1 keeps a single connection: modernc.org/sqlite serializes
	// writes at the connection level, and a single Store is meant to
	// back one `eng` process at a time in v1 anyway.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: enable foreign keys: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close implements kernel.Storage.
func (s *Store) Close() error { return s.db.Close() }

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// nullTime converts a zero time.Time to SQL NULL, so "never happened"
// isn't confused with the SQLite epoch.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// scanTime reads a nullable timestamp column into a time.Time, zero if
// the column was NULL.
func scanTime(ns sql.NullTime) time.Time {
	if !ns.Valid {
		return time.Time{}
	}
	return ns.Time
}
