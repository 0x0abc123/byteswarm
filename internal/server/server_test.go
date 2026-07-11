package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthAndReadinessEndpoints(t *testing.T) {
	h := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), &fakePublisher{})

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
