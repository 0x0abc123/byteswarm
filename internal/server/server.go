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
)

// New builds the HTTP handler: the health/readiness endpoints plus the
// standard middleware chain. Business routes (the webhook ingress, authorized
// through the auth port) are added as features land.
func New(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", writeStatus("ok"))
	mux.HandleFunc("GET /readyz", writeStatus("ready"))
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
