package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/0x0abc123/byteswarm/internal/event"
)

// Boundary limits for a submitted event (reference/security-fundamentals.md:
// validate and bound ALL external input). Type/WorkflowID also become NATS
// subject tokens (ADR-0004), so they reject whitespace and wildcards here to
// fail fast with a 400 rather than deep in the bus adapter.
const (
	maxEventBodyBytes = 1 << 20 // 1 MiB total request body
	maxWorkflowIDLen  = 128
)

// submitRequest is the JSON body accepted by POST /events. Payload is kept raw
// (opaque bytes to the ingress) and carried through to the event.
type submitRequest struct {
	Type       string          `json:"type"`
	WorkflowID string          `json:"workflowID"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// submitEvent handles POST /events: the operator-local event ingress. It
// simply accepts the event (no auth — the CLI is operator-local per ADR-0002).
func submitEvent(logger *slog.Logger, pub event.Publisher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acceptEvent(w, r, logger, pub)
	}
}

// acceptEvent decodes and bounds the request body, validates the event at the
// boundary, publishes via the Publisher port, and writes 202 Accepted. Nothing
// is published unless validation passes (fail closed). Shared by the CLI-facing
// /events ingress and the authenticated /webhook ingress.
func acceptEvent(w http.ResponseWriter, r *http.Request, logger *slog.Logger, pub event.Publisher) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEventBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req submitRequest
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "event body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid event body")
		return
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "body must contain a single JSON event")
		return
	}
	if msg, ok := validateSubmit(req); !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	e := event.Event{Type: req.Type, WorkflowID: req.WorkflowID, Payload: req.Payload}
	if err := pub.Publish(r.Context(), e); err != nil {
		logger.ErrorContext(r.Context(), "server: publishing submitted event failed",
			slog.String("event_type", e.Type),
			slog.String("correlation_id", CorrelationIDFrom(r.Context())),
			slog.String("err", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "could not accept event")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}

// validateSubmit enforces the boundary invariants; the bool is false with a
// safe, non-leaking message when the request is rejected.
func validateSubmit(req submitRequest) (string, bool) {
	switch {
	case !event.ValidType(req.Type):
		return "invalid event type (single token [A-Za-z0-9_-], no dots)", false
	case len(req.WorkflowID) > maxWorkflowIDLen:
		return "workflowID too long", false
	case req.WorkflowID != "" && !validWorkflowID(req.WorkflowID):
		return "workflowID contains illegal characters", false
	}
	return "", true
}

// validWorkflowID rejects whitespace and NATS subject wildcards. The workflowID
// is the trailing subject token and stays flexible (ADR-0010 constrains only
// the type); event.ValidType is the single rule for the type token.
func validWorkflowID(s string) bool {
	return !strings.ContainsAny(s, " \t\r\n*>")
}

// writeError emits a small JSON error body with the given status. The message
// is caller-controlled and must not leak internal detail.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body, _ := json.Marshal(map[string]string{"error": msg})
	_, _ = w.Write(body)
}
