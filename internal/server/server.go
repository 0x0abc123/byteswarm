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

	"github.com/0x0abc123/byteswarm/internal/auth"
	"github.com/0x0abc123/byteswarm/internal/event"
)

// New builds the HTTP handler: the health/readiness endpoints, the operator
// event submit endpoint (/events), the authenticated external-trigger webhook
// (/webhook), and the standard middleware chain. The Publisher and
// Authenticator ports are injected (constructor injection, wired at the
// composition root).
func New(logger *slog.Logger, pub event.Publisher, webhookAuth auth.Authenticator) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", writeStatus("ok"))
	mux.HandleFunc("GET /readyz", writeStatus("ready"))
	mux.HandleFunc("POST /events", submitEvent(logger, pub))
	mux.HandleFunc("POST /webhook", webhook(logger, webhookAuth, pub))
	// Order (outer→inner): correlationID sets the ID first, then requestLogger,
	// then recoverer wraps the handlers — so a recovered panic is logged with
	// the correlation ID already in context.
	return correlationID(requestLogger(logger)(recoverer(logger)(mux)))
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
