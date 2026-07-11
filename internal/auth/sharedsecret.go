package auth

import (
	"context"
	"crypto/subtle"
)

// SharedSecret authenticates a caller by comparing a presented credential to a
// single configured secret — the default webhook auth mechanism (ADR-0002). It
// fails closed: a mismatch, an empty credential, or an empty configured secret
// all deny. The comparison is constant-time to avoid leaking the secret via
// response timing.
type SharedSecret struct {
	secret []byte
}

// compile-time proof the adapter satisfies the auth port.
var _ Authenticator = (*SharedSecret)(nil)

// NewSharedSecret builds a shared-secret authenticator. The secret is supplied
// by the composition root (from env/config, never hard-coded); an empty secret
// yields an authenticator that denies everything (deny by default).
func NewSharedSecret(secret string) *SharedSecret {
	return &SharedSecret{secret: []byte(secret)}
}

// Authenticate returns nil only when credential equals the configured secret.
// An empty configured secret or an empty credential always denies. Neither the
// secret nor the credential is logged (reference/security-fundamentals.md).
func (s *SharedSecret) Authenticate(_ context.Context, credential string) error {
	if len(s.secret) == 0 || credential == "" {
		return ErrUnauthenticated
	}
	if subtle.ConstantTimeCompare([]byte(credential), s.secret) != 1 {
		return ErrUnauthenticated
	}
	return nil
}
