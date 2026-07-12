package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0x0abc123/byteswarm/internal/auth"
)

func TestHealthAndReadinessEndpoints(t *testing.T) {
	h := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), &fakePublisher{}, auth.NewSharedSecret("test-secret")).Control

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want %d", path, rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("X-Correlation-ID"); got == "" {
			t.Errorf("GET %s: missing X-Correlation-ID header", path)
		}
	}
}

// The ingress is split by transport (ADR-0011): /events lives ONLY on the
// Events handler (Unix socket) and /webhook + health live ONLY on the Control
// handler (TCP). A route reaching the wrong handler would mean the composition
// root could accidentally expose the unauthenticated /events over TCP, or drop
// the authenticated /webhook.
func TestIngressRoutesAreSplitByHandler(t *testing.T) {
	h := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), &fakePublisher{}, auth.NewSharedSecret("test-secret"))

	cases := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
	}{
		{"events not on control", h.Control, http.MethodPost, "/events"},
		{"webhook not on events", h.Events, http.MethodPost, "/webhook"},
		{"health not on events", h.Events, http.MethodGet, "/healthz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"type":"t"}`))
			rec := httptest.NewRecorder()
			tc.handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s %s reached the wrong handler: status = %d, want %d", tc.method, tc.path, rec.Code, http.StatusNotFound)
			}
		})
	}
}
