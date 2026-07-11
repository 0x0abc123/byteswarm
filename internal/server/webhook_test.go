package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0x0abc123/byteswarm/internal/auth"
	"github.com/0x0abc123/byteswarm/internal/event"
)

func webhookTestHandler(pub event.Publisher, secret string) http.Handler {
	return New(slog.New(slog.NewJSONHandler(io.Discard, nil)), pub, auth.NewSharedSecret(secret))
}

func TestWebhookAuthenticatedPublishes(t *testing.T) {
	pub := &fakePublisher{}
	h := webhookTestHandler(pub, "s3cret")

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"type":"order_created","workflowID":"wf1"}`))
	req.Header.Set("Authorization", "Bearer s3cret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if pub.count() != 1 {
		t.Fatalf("published %d events, want 1", pub.count())
	}
}

func TestWebhookRejectsBadAuth(t *testing.T) {
	tests := []struct {
		name   string
		header string // Authorization header value; "" means omit
	}{
		{"missing header", ""},
		{"wrong secret", "Bearer nope"},
		{"not a bearer scheme", "Basic s3cret"},
		{"empty bearer token", "Bearer "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			h := webhookTestHandler(pub, "s3cret")

			req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"type":"t"}`))
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if pub.count() != 0 {
				t.Fatalf("published %d events, want 0 (unauthenticated must not publish)", pub.count())
			}
		})
	}
}

func TestWebhookAuthenticatedButBadBody(t *testing.T) {
	pub := &fakePublisher{}
	h := webhookTestHandler(pub, "s3cret")

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"type":`)) // malformed
	req.Header.Set("Authorization", "Bearer s3cret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if pub.count() != 0 {
		t.Fatalf("published %d events, want 0", pub.count())
	}
}

// An empty configured webhook secret denies everything (fail closed).
func TestWebhookEmptySecretDeniesAll(t *testing.T) {
	pub := &fakePublisher{}
	h := webhookTestHandler(pub, "")

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"type":"t"}`))
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if pub.count() != 0 {
		t.Fatalf("published %d events, want 0", pub.count())
	}
}
