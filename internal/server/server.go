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

// Handlers are the two inbound HTTP handlers, each bound to its own transport by
// the composition root (ADR-0011). Events carries the operator-local POST
// /events and is served over a Unix domain socket whose file permissions are the
// access control — it has no application-layer auth. Control carries the
// authenticated POST /webhook and the health endpoints and is served over TCP
// for untrusted and cross-host callers. Both funnel into the same
// bounded/validated acceptEvent path.
type Handlers struct {
	Events  http.Handler
	Control http.Handler
}

// New builds the two inbound HTTP handlers with the shared middleware chain. The
// Publisher and Authenticator ports are injected (constructor injection, wired
// at the composition root). Splitting the routes by handler is what lets the
// root serve /events on the Unix socket and /webhook + health on TCP.
func New(logger *slog.Logger, pub event.Publisher, webhookAuth auth.Authenticator) Handlers {
	events := http.NewServeMux()
	events.HandleFunc("POST /events", submitEvent(logger, pub))

	control := http.NewServeMux()
	control.HandleFunc("GET /healthz", writeStatus("ok"))
	control.HandleFunc("GET /readyz", writeStatus("ready"))
	control.HandleFunc("POST /webhook", webhook(logger, webhookAuth, pub))

	return Handlers{
		Events:  withMiddleware(logger, events),
		Control: withMiddleware(logger, control),
	}
}

// withMiddleware wraps a handler in the standard chain. Order (outer→inner):
// correlationID sets the ID first, then requestLogger, then recoverer wraps the
// handler — so a recovered panic is logged with the correlation ID already in
// context.
func withMiddleware(logger *slog.Logger, h http.Handler) http.Handler {
	return correlationID(requestLogger(logger)(recoverer(logger)(h)))
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
