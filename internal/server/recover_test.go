package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecovererCatchesPanicAndKeepsServing(t *testing.T) {
	var logbuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logbuf, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) { panic("boom-with-secret-detail") })
	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// Same chain New builds, so the correlation ID is set before recoverer runs.
	h := correlationID(requestLogger(logger)(recoverer(logger)(mux)))

	// A panicking handler → 500 (not a dropped connection), logged with the
	// correlation ID, and the panic detail must not leak into the response.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic route status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "boom-with-secret-detail") {
		t.Fatalf("response leaked panic detail: %s", rec.Body.String())
	}
	logs := logbuf.String()
	if !strings.Contains(logs, "recovered panic in handler") {
		t.Fatalf("no structured panic log: %s", logs)
	}
	if !strings.Contains(logs, `"correlation_id"`) {
		t.Fatalf("panic log missing correlation_id: %s", logs)
	}

	// The server keeps serving subsequent requests.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("ok route status = %d, want %d (server must keep serving after a panic)", rec2.Code, http.StatusOK)
	}
}
