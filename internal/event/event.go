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
)

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
