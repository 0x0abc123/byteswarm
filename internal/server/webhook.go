package server

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/0x0abc123/byteswarm/internal/auth"
	"github.com/0x0abc123/byteswarm/internal/event"
)

// webhook handles POST /webhook: the authenticated ingress for external trigger
// sources (ADR-0002). It authenticates the caller with the shared-secret
// Authenticator before doing anything else — deny by default: on any auth
// failure it returns 401 and publishes nothing. On success it accepts the event
// through the same bounded/validated path as /events.
//
// The credential is presented as `Authorization: Bearer <secret>`.
func webhook(logger *slog.Logger, authr auth.Authenticator, pub event.Publisher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := authr.Authenticate(r.Context(), bearerToken(r)); err != nil {
			// Log the denial as a security event — never the credential.
			logger.WarnContext(r.Context(), "server: webhook authentication failed",
				slog.String("correlation_id", CorrelationIDFrom(r.Context())),
				slog.String("remote_addr", r.RemoteAddr),
			)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		acceptEvent(w, r, logger, pub)
	}
}

// bearerToken extracts the token from an `Authorization: Bearer <token>`
// header, or "" if absent/malformed (an empty credential is denied by the
// fail-closed Authenticator).
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}
