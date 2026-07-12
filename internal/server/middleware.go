package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

type ctxKey string

const correlationIDKey ctxKey = "correlation_id"

// correlationID ensures every request carries a correlation ID, taken from the
// inbound X-Correlation-ID header or freshly generated, propagated through the
// request context and echoed on the response.
func correlationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if id == "" {
			id = newCorrelationID()
		}
		w.Header().Set("X-Correlation-ID", id)
		ctx := context.WithValue(r.Context(), correlationIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestLogger emits one structured log line per request, carrying the
// correlation ID so it can be traced through ports and adapters.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.InfoContext(r.Context(), "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("correlation_id", CorrelationIDFrom(r.Context())),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// recoverer catches a panic from any downstream handler, logs it as a
// structured error record (with the correlation ID), and returns a generic 500
// — so a handler bug becomes a logged error and a clean response instead of a
// dropped connection, and the server keeps serving. The response carries no
// panic detail or stack, and the request body is never logged
// (reference/security-fundamentals.md).
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "server: recovered panic in handler",
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.String("correlation_id", CorrelationIDFrom(r.Context())),
						slog.Any("panic", rec),
					)
					writeError(w, http.StatusInternalServerError, "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CorrelationIDFrom returns the correlation ID carried on ctx, or "" if absent.
func CorrelationIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(correlationIDKey).(string); ok {
		return v
	}
	return ""
}

func newCorrelationID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}
