// Package telemetry declares the outbound business-event emitter port
// (ADR-0001 telemetry boundary). Consumers emit business events through this
// port to consumer-defined sinks. Structured logging (log/slog) is a separate
// concern configured in the composition root and applied in adapters and
// middleware, never inside domain logic (reference/design-principles.md).
package telemetry

import "context"

// Emitter publishes a named business event with structured attributes to a
// downstream sink. Kept behind a port so the sink is swappable and the domain
// stays free of transport concerns.
type Emitter interface {
	Emit(ctx context.Context, name string, attrs map[string]any) error
}
