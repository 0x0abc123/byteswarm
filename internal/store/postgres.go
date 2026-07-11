package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql driver — pure Go, keeps CGO_ENABLED=0

	"github.com/0x0abc123/byteswarm/internal/consumer"
)

// PostgresStore is the production Repository adapter (ADR-0005, ADR-0009). It
// stores opaque consumer state in a BYTEA column via database/sql + pgx. State
// is opaque bytes — not JSONB — so it round-trips any value the port carries
// (ADR-0009 supersedes ADR-0005's JSONB decision on this dimension).
type PostgresStore struct {
	db *sql.DB
}

// compile-time proof the adapter satisfies the domain port.
var _ consumer.Repository = (*PostgresStore)(nil)

// NewPostgres opens a pooled connection to dsn and ensures the schema. Secrets
// live in the DSN, supplied via env/config, never hard-coded
// (reference/security-fundamentals.md).
func NewPostgres(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: opening postgres: %w", err)
	}
	if err := ensurePostgresSchema(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &PostgresStore{db: db}, nil
}

const createConsumerStatePG = `
CREATE TABLE IF NOT EXISTS consumer_state (
	id         TEXT PRIMARY KEY,
	state      BYTEA NOT NULL,
	updated_at BIGINT NOT NULL
)`

func ensurePostgresSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createConsumerStatePG); err != nil {
		return fmt.Errorf("store: ensuring postgres schema: %w", err)
	}
	return nil
}

// Load returns the stored state for id, or (nil, nil) if no state exists yet.
func (s *PostgresStore) Load(ctx context.Context, id string) ([]byte, error) {
	var state []byte
	err := s.db.QueryRowContext(ctx, `SELECT state FROM consumer_state WHERE id = $1`, id).Scan(&state)
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
func (s *PostgresStore) Save(ctx context.Context, id string, state []byte) error {
	if state == nil {
		state = []byte{} // keep the NOT NULL column deterministic
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO consumer_state (id, state, updated_at) VALUES ($1, $2, $3)
		 ON CONFLICT (id) DO UPDATE SET state = EXCLUDED.state, updated_at = EXCLUDED.updated_at`,
		id, state, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("store: saving %q: %w", id, err)
	}
	return nil
}

// Close releases the connection pool.
func (s *PostgresStore) Close() error { return s.db.Close() }
