package auth

import (
	"context"
	"errors"
	"testing"
)

func TestSharedSecretAuthenticate(t *testing.T) {
	tests := []struct {
		name       string
		secret     string
		credential string
		wantErr    bool
	}{
		{"correct secret", "s3cret", "s3cret", false},
		{"wrong credential", "s3cret", "nope", true},
		{"empty credential", "s3cret", "", true},
		{"prefix is not a match", "s3cret", "s3c", true},
		{"empty secret denies non-empty credential", "", "anything", true},
		{"empty secret denies empty credential", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := NewSharedSecret(tc.secret)
			err := a.Authenticate(context.Background(), tc.credential)
			if tc.wantErr {
				if !errors.Is(err, ErrUnauthenticated) {
					t.Fatalf("Authenticate(%q) error = %v, want ErrUnauthenticated", tc.credential, err)
				}
			} else if err != nil {
				t.Fatalf("Authenticate(%q) error = %v, want nil", tc.credential, err)
			}
		})
	}
}
