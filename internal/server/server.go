// Package server is byteswarm's inbound HTTP adapter (ADR-0002 webhook
// ingress). It exposes the operational /healthz and /readyz endpoints that
// make the artifact portable across orchestrators, and wraps every request in
// the correlation-ID and structured-logging middleware required by the
// observability baseline (reference/design-principles.md). It depends on
// domain ports; the domain never depends on it.
package server

import (
	"log/slog"
	"net/http"

	"github.com/0x0abc123/byteswarm/internal/event"
)

// New builds the HTTP handler: the health/readiness endpoints, the event
// submit endpoint, and the standard middleware chain. The Publisher port is
// injected (constructor injection, wired at the composition root); the
// authenticated external-trigger webhook is added in a later feature.
func New(logger *slog.Logger, pub event.Publisher) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", writeStatus("ok"))
	mux.HandleFunc("GET /readyz", writeStatus("ready"))
	mux.HandleFunc("POST /events", submitEvent(logger, pub))
	return correlationID(requestLogger(logger)(mux))
}

// writeStatus returns a handler emitting a small JSON status body.
func writeStatus(status string) http.HandlerFunc {
	body := []byte(`{"status":"` + status + `"}`)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body) // response write errors are not actionable here
	}
}
