// Package store holds byteswarm's outbound persistence adapters implementing
// the consumer.Repository port (ADR-0005): SQLite (embedded, zero-infra) and —
// as it lands — PostgreSQL (production). Only this package touches a database;
// the domain depends on the port, never on store (dependency direction is
// one-way, reference/design-principles.md).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver — no cgo, keeps CGO_ENABLED=0 (ADR-0006)

	"github.com/0x0abc123/byteswarm/internal/consumer"
)

// SQLiteStore is the embedded SQLite adapter for consumer.Repository — the
// zero-extra-infrastructure option for small / single-node / dev deployments
// (ADR-0005).
type SQLiteStore struct {
	db *sql.DB
}

// compile-time proof the adapter satisfies the domain port.
var _ consumer.Repository = (*SQLiteStore)(nil)

// NewSQLite opens (or creates) the SQLite database at path and ensures the
// schema. Pass ":memory:" for an ephemeral store.
func NewSQLite(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: opening sqlite %q: %w", path, err)
	}
	// SQLite allows a single writer; cap the pool at one connection to avoid
	// spurious "database is locked" errors under database/sql's pooling.
	db.SetMaxOpenConns(1)
	if err := ensureSchema(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

const createConsumerState = `
CREATE TABLE IF NOT EXISTS consumer_state (
	id         TEXT PRIMARY KEY,
	state      BLOB NOT NULL,
	updated_at INTEGER NOT NULL
)`

func ensureSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createConsumerState); err != nil {
		return fmt.Errorf("store: ensuring schema: %w", err)
	}
	return nil
}

// Load returns the stored state for id, or (nil, nil) if no state exists yet.
func (s *SQLiteStore) Load(ctx context.Context, id string) ([]byte, error) {
	var state []byte
	err := s.db.QueryRowContext(ctx, `SELECT state FROM consumer_state WHERE id = ?`, id).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: loading %q: %w", id, err)
	}
	return state, nil
}

// Save upserts the state for id. Parameterised query only (no string-built SQL,
// reference/security-fundamentals.md).
func (s *SQLiteStore) Save(ctx context.Context, id string, state []byte) error {
	if state == nil {
		state = []byte{} // keep the NOT NULL column deterministic
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO consumer_state (id, state, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET state = excluded.state, updated_at = excluded.updated_at`,
		id, state, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("store: saving %q: %w", id, err)
	}
	return nil
}

// Close releases the database handle.
func (s *SQLiteStore) Close() error { return s.db.Close() }
