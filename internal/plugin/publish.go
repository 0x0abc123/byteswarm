package plugin

import (
	"context"

	"github.com/0x0abc123/byteswarm/internal/event"
)

// PublishCapability is the script `publish` capability: it lets a plugin emit
// derived events through the existing event.Publisher port (ADR-0004/0008).
// Script-supplied event fields are untrusted and validated at this host
// boundary before the event reaches the bus.
type PublishCapability struct {
	pub event.Publisher
}

// NewPublishCapability binds the capability to an event publisher.
func NewPublishCapability(pub event.Publisher) *PublishCapability {
	return &PublishCapability{pub: pub}
}

// Publish validates a script-supplied event at the host boundary and forwards
// it. The forwarding is real; the field validation is attached with the goja
// runtime (the shape of script-supplied values is known only then).
func (c *PublishCapability) Publish(ctx context.Context, e event.Event) error {
	// TODO(code-migration): validate script-supplied Type/WorkflowID/Payload
	// (bounds, subject charset) before publishing (ADR-0008 untrusted input).
	return c.pub.Publish(ctx, e)
}
