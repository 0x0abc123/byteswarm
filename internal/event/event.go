// Package event defines byteswarm's core event model and the routing ports
// the domain consumes (ADR-0001 event model & routing core; ADR-0004 Event
// Bus). Ports are declared here, at the consumer; adapters (the NATS bus, the
// HTTP/CLI ingress) implement or call them. This package imports no adapter
// code — the dependency direction is one-way (reference/design-principles.md).
package event

import "context"

// Subject-scheme constants for the event bus (ADR-0004). The routing contract
// lives here in the domain so the bus adapter and in-process routers agree on
// one encoding; the bus adapter owns the full per-event encoding.
const (
	// SubjectPrefix namespaces every byteswarm event subject
	// (bw.evt.<type>.<workflowID>).
	SubjectPrefix = "bw.evt"
	// SubjectAll matches every byteswarm event (any type, any workflowID) —
	// the subscription an any-scope instance uses. WorkflowID-scoped
	// subscriptions are F4.4.
	SubjectAll = SubjectPrefix + ".>"

	// BroadcastType is the reserved event type for systemwide notices —
	// delivered to every consumer (see internal/consumer). It intentionally
	// uses '@' so no ordinary (charset-restricted) type can collide with it;
	// ValidType exempts it (ADR-0010).
	BroadcastType = "@broadcast"

	// maxTypeLen bounds an event type token.
	maxTypeLen = 128
)

// ValidType reports whether t is a legal event type (ADR-0010): a single
// subject token — non-empty, at most maxTypeLen, charset [A-Za-z0-9_-] with no
// dots/whitespace/NATS-wildcards — or the reserved BroadcastType sentinel. A
// dot-free single token keeps <type> at a fixed subject position so a
// workflowID can be pinned by a wildcard (bw.evt.*.<workflowID>). This is the
// single source of truth shared by the bus, HTTP ingress, and plugin publish.
func ValidType(t string) bool {
	if t == BroadcastType {
		return true
	}
	if t == "" || len(t) > maxTypeLen {
		return false
	}
	for _, r := range t {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// Event is the unit of work byteswarm publishes, routes, and delivers.
// Subjects encode Type and WorkflowID (e.g. bw.evt.<type>.<workflowID>, ADR-0004).
type Event struct {
	Type       string
	WorkflowID string
	Payload    []byte
}

// Bus is the outbound port for publishing and subscribing to events.
// Default adapter: NATS JetStream (ADR-0004); the port keeps the broker
// swappable and keeps the domain free of transport concerns.
type Bus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(ctx context.Context, subject string, handle func(context.Context, Event) error) error
}

// Publisher is the inbound port ingress adapters (webhook receiver, CLI
// publisher — ADR-0002) call to produce events into the core.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}
