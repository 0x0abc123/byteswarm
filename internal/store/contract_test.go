package store

import (
	"bytes"
	"context"
	"testing"

	"github.com/0x0abc123/byteswarm/internal/consumer"
)

// runRepositoryContract exercises the behavior every consumer.Repository
// adapter must share (ADR-0005), so SQLite and PostgreSQL are provably
// interchangeable behind the port. F2.2's PostgreSQL test reuses this.
func runRepositoryContract(t *testing.T, repo consumer.Repository) {
	t.Helper()
	ctx := context.Background()

	// A missing key yields (nil, nil) — "no state yet".
	got, err := repo.Load(ctx, "absent")
	if err != nil {
		t.Fatalf("Load(absent) error: %v", err)
	}
	if got != nil {
		t.Fatalf("Load(absent) = %q, want nil", got)
	}

	// Save then Load round-trips the bytes.
	if err := repo.Save(ctx, "k1", []byte("v1")); err != nil {
		t.Fatalf("Save(k1): %v", err)
	}
	if got, _ = repo.Load(ctx, "k1"); !bytes.Equal(got, []byte("v1")) {
		t.Fatalf("Load(k1) = %q, want v1", got)
	}

	// Saving the same key again overwrites.
	if err := repo.Save(ctx, "k1", []byte("v2")); err != nil {
		t.Fatalf("Save(k1) overwrite: %v", err)
	}
	if got, _ = repo.Load(ctx, "k1"); !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("Load(k1) after overwrite = %q, want v2", got)
	}

	// Keys are independent.
	if err := repo.Save(ctx, "k2", []byte("other")); err != nil {
		t.Fatalf("Save(k2): %v", err)
	}
	if got, _ = repo.Load(ctx, "k1"); !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("Load(k1) disturbed by k2 write = %q, want v2", got)
	}
}
