package plugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/0x0abc123/byteswarm/internal/event"
)

// ErrInvalidEvent is returned when a script tries to publish an event whose
// fields fail host-boundary validation (ADR-0008: script-supplied values are
// untrusted input).
var ErrInvalidEvent = errors.New("plugin: invalid event from script")

// Bounds for script-supplied event fields. Type and WorkflowID become NATS
// subject tokens (ADR-0004 subject: bw.evt.<type>.<workflowID>), so their
// charset is restricted to prevent subject injection.
const (
	maxEventTypeLen      = 128
	maxWorkflowIDLen     = 128
	maxScriptPayloadByte = 1 << 20 // 1 MiB
)

// validSubjectToken reports whether s is a safe single NATS subject token:
// non-empty runs of [A-Za-z0-9_-]. It excludes '.' (the subject separator),
// wildcards ('*', '>'), and whitespace.
func validSubjectToken(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

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

// Publish validates a script-supplied event at the host boundary — subject
// charset and field bounds — then forwards it via the event.Publisher port. It
// fails closed: an event that fails validation is never published.
func (c *PublishCapability) Publish(ctx context.Context, e event.Event) error {
	if e.Type == "" || len(e.Type) > maxEventTypeLen || !validSubjectToken(e.Type) {
		return fmt.Errorf("%w: type", ErrInvalidEvent)
	}
	if len(e.WorkflowID) > maxWorkflowIDLen || (e.WorkflowID != "" && !validSubjectToken(e.WorkflowID)) {
		return fmt.Errorf("%w: workflowID", ErrInvalidEvent)
	}
	if len(e.Payload) > maxScriptPayloadByte {
		return fmt.Errorf("%w: payload too large", ErrInvalidEvent)
	}
	return c.pub.Publish(ctx, e)
}
