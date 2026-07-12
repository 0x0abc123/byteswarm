package telemetry

import (
	"context"
	"errors"
	"log/slog"
)

// ErrEmptyEventName is returned by Emit when the business-event name is empty.
var ErrEmptyEventName = errors.New("telemetry: business event name is required")

// SlogEmitter is the baseline Emitter adapter: it records each business event
// as one structured log line (reference/design-principles.md: logs first;
// OpenTelemetry only if a future ADR records the need). The *slog.Logger is the
// sink seam — a real external / consumer-defined sink (ADR-0006) can replace it
// behind the Emitter port without changing callers.
type SlogEmitter struct {
	log *slog.Logger
}

// compile-time proof the adapter satisfies the domain port.
var _ Emitter = (*SlogEmitter)(nil)

// NewSlogEmitter builds an emitter that writes business events to log; a nil
// logger falls back to slog.Default().
func NewSlogEmitter(log *slog.Logger) *SlogEmitter {
	if log == nil {
		log = slog.Default()
	}
	return &SlogEmitter{log: log}
}

// Emit records one business event: the name plus the caller-supplied attrs,
// grouped under "attrs" and tagged with a stable "event" field so business
// events are greppable and distinct from ordinary logs. A missing name is
// rejected and nothing is emitted.
//
// attrs are caller-controlled and MUST NOT carry secrets, credentials, or
// personal data (reference/security-fundamentals.md: never log credentials or
// personal data). The emitter passes them through verbatim — sanitising is the
// caller's responsibility.
func (e *SlogEmitter) Emit(ctx context.Context, name string, attrs map[string]any) error {
	if name == "" {
		return ErrEmptyEventName
	}
	grouped := make([]any, 0, len(attrs))
	for k, v := range attrs {
		grouped = append(grouped, slog.Any(k, v))
	}
	e.log.LogAttrs(ctx, slog.LevelInfo, "business event",
		slog.String("event", name),
		slog.Group("attrs", grouped...),
	)
	return nil
}
