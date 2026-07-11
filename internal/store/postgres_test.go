package store

import (
	"os"
	"testing"
)

// TestPostgresStoreContract runs the shared Repository contract against a real
// PostgreSQL instance. It is gated on BYTESWARM_TEST_PG_DSN and skips cleanly
// when unset (there is no pure-Go embedded Postgres, unlike SQLite/NATS), so
// `go test ./...` stays green locally; CI provides the DSN + a Postgres service.
func TestPostgresStoreContract(t *testing.T) {
	dsn := os.Getenv("BYTESWARM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("BYTESWARM_TEST_PG_DSN not set; skipping PostgreSQL contract test")
	}

	s, err := NewPostgres(dsn)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// The contract test uses fixed keys; start from a clean table so a prior
	// run cannot leak state into the missing-key assertion.
	if _, err := s.db.ExecContext(t.Context(), `DELETE FROM consumer_state`); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	runRepositoryContract(t, s)
}
