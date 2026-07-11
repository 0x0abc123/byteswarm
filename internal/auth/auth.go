// Package auth declares the authentication port ingress adapters use to
// authorize inbound requests (ADR-0002). The default adapter is shared-secret;
// stronger mechanisms (SSO/JWT/mTLS) can replace it without touching the core.
// Per reference/security-fundamentals.md the auth path denies by default and
// fails closed.
package auth

import (
	"context"
	"errors"
)

// ErrUnauthenticated is returned when a credential is missing or invalid.
// Callers must treat any non-nil error as a denial (fail closed).
var ErrUnauthenticated = errors.New("auth: unauthenticated")

// Authenticator verifies an inbound credential. Implementations must fail
// closed: an empty credential or any verification error denies access.
type Authenticator interface {
	Authenticate(ctx context.Context, credential string) error
}
