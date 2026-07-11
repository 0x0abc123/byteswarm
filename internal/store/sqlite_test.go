package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteStoreContract(t *testing.T) {
	s, err := NewSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	runRepositoryContract(t, s)
}

func TestSQLiteStorePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")

	s1, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	if err := s1.Save(context.Background(), "durable", []byte("kept")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_ = s1.Close()

	s2, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("reopen NewSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	got, err := s2.Load(context.Background(), "durable")
	if err != nil {
		t.Fatalf("Load after reopen: %v", err)
	}
	if string(got) != "kept" {
		t.Fatalf("Load after reopen = %q, want %q (state must survive restart, ADR-0005)", got, "kept")
	}
}
