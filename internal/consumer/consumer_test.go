package consumer

import (
	"context"
	"testing"
)

// staticRepo is an in-memory fake proving the Repository port is satisfiable
// and injectable through its interface.
type staticRepo struct{ state []byte }

func (r staticRepo) Load(context.Context, string) ([]byte, error) { return r.state, nil }
func (r staticRepo) Save(context.Context, string, []byte) error   { return nil }

func TestRepositoryInterfaceSatisfied(t *testing.T) {
	var repo Repository = staticRepo{state: []byte("ok")}
	got, err := repo.Load(context.Background(), "id")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("Load = %q, want %q", got, "ok")
	}
}
