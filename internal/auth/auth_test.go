package auth

import (
	"context"
	"errors"
	"testing"
)

// denyAll is a fail-closed Authenticator used to prove the port's contract:
// any credential is denied.
type denyAll struct{}

func (denyAll) Authenticate(context.Context, string) error { return ErrUnauthenticated }

func TestDenyByDefault(t *testing.T) {
	var a Authenticator = denyAll{}
	if err := a.Authenticate(context.Background(), "any"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated, got %v", err)
	}
}
