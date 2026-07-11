package main

import (
	"context"
	"log/slog"

	"github.com/0x0abc123/byteswarm/internal/consumer"
	"github.com/0x0abc123/byteswarm/internal/event"
)

// exampleEventType is the event type the built-in demo consumer subscribes to.
// It closes the M1 tracer bullet: `byteswarmctl publish --type demo` reaches
// this consumer over NATS.
const exampleEventType = "demo"

// exampleConsumer is a built-in demo consumer that logs each event it handles.
// It proves the end-to-end dispatch path (F1.5) and serves as a template for
// real compiled-in Go consumers; replace it once real consumers exist.
type exampleConsumer struct{ log *slog.Logger }

// compile-time proof the demo consumer satisfies the dispatch port.
var _ consumer.Consumer = (*exampleConsumer)(nil)

func newExampleConsumer(log *slog.Logger) *exampleConsumer {
	return &exampleConsumer{log: log}
}

// Handle logs the delivered event and succeeds. It logs only metadata (type,
// workflowID, payload size) — never the payload body, which may carry
// sensitive data (reference/security-fundamentals.md).
func (c *exampleConsumer) Handle(ctx context.Context, e event.Event) error {
	c.log.InfoContext(ctx, "example consumer handled event",
		slog.String("type", e.Type),
		slog.String("workflowID", e.WorkflowID),
		slog.Int("payload_bytes", len(e.Payload)),
	)
	return nil
}
