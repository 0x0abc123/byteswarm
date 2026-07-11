package consumer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/0x0abc123/byteswarm/internal/event"
)

// BroadcastType is the reserved event type for systemwide notices: an event
// published with this type is delivered to every registered consumer,
// regardless of its specific subscriptions (architecture brief "global
// broadcast"). Consumers need not register for it explicitly. The canonical
// value lives in the event package (the routing contract, ADR-0010).
const BroadcastType = event.BroadcastType

// Registry maps event types to the Consumers subscribed to them and dispatches
// each delivered event to its subscribers, in isolation. It is the in-process
// router between the bus (event.Bus port) and the Consumer port; both native
// Go consumers and script consumers register here.
//
// Routing is in-process by the event's Type: Run takes one broad subscription
// (event.SubjectAll) and fans out. Broker-side / workflowID-scoped
// subscriptions are F4.4.
type Registry struct {
	log *slog.Logger

	mu     sync.RWMutex
	byType map[string][]Consumer
	all    []Consumer // every registered consumer, for broadcast delivery
}

// NewRegistry constructs an empty registry.
func NewRegistry(log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{log: log, byType: make(map[string][]Consumer)}
}

// Register subscribes c to each of the given event types. Register a consumer
// once with all of its event types; a consumer does not need BroadcastType —
// broadcast events reach every registered consumer regardless.
func (r *Registry) Register(c Consumer, events ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range events {
		r.byType[t] = append(r.byType[t], c)
	}
	r.all = append(r.all, c)
}

// Subscriber is a Consumer that declares its own event subscriptions (e.g.
// plugin.ScriptConsumer). RegisterSubscriber is a convenience over Register.
type Subscriber interface {
	Consumer
	Events() []string
}

// RegisterSubscriber registers s for the event types it declares via Events().
func (r *Registry) RegisterSubscriber(s Subscriber) {
	r.Register(s, s.Events()...)
}

// Run subscribes to the event stream on the given bus and dispatches every
// delivery until ctx is cancelled. At-least-once (F4.1): the delivery handler
// returns an error when any subscribed consumer failed, so the bus Naks and the
// event is redelivered; consumers must be idempotent (a redelivered event
// re-runs the type's consumers — a stated non-functional in the brief). The
// bus bounds redelivery so a permanently-failing event cannot loop forever.
func (r *Registry) Run(ctx context.Context, bus event.Bus) error {
	err := bus.Subscribe(ctx, event.SubjectAll, func(hctx context.Context, e event.Event) error {
		return r.dispatch(hctx, e)
	})
	if err != nil {
		return fmt.Errorf("consumer: subscribing registry: %w", err)
	}
	<-ctx.Done()
	return nil
}

// dispatch routes one event to the consumers subscribed to its type (or to all
// consumers for a broadcast), each isolated so one consumer's error or panic
// cannot affect the others (ADR-0001). It returns a non-nil error if any
// consumer failed, so the caller can trigger redelivery.
func (r *Registry) dispatch(ctx context.Context, e event.Event) error {
	r.mu.RLock()
	var targets []Consumer
	if e.Type == BroadcastType {
		targets = append(targets, r.all...)
	} else {
		targets = append(targets, r.byType[e.Type]...)
	}
	r.mu.RUnlock()

	var failed int
	for _, c := range targets {
		if err := r.safeHandle(ctx, c, e); err != nil {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("consumer: %d of %d handler(s) failed for event %q", failed, len(targets), e.Type)
	}
	return nil
}

// safeHandle invokes one consumer, recovering panics so a misbehaving consumer
// cannot down the instance or stop its siblings. It returns a non-nil error
// (logged) when the consumer errored or panicked, so dispatch can count the
// failure toward redelivery.
func (r *Registry) safeHandle(ctx context.Context, c Consumer, e event.Event) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("consumer: recovered panic in handler",
				"consumer", consumerName(c), "event_type", e.Type, "panic", rec)
			err = fmt.Errorf("consumer %s panicked: %v", consumerName(c), rec)
		}
	}()
	if err := c.Handle(ctx, e); err != nil {
		r.log.Error("consumer: handler returned error",
			"consumer", consumerName(c), "event_type", e.Type, "err", err)
		return err
	}
	return nil
}

// consumerName prefers a consumer's declared Name() (e.g. a script plugin) and
// falls back to its Go type, for log attribution only.
func consumerName(c Consumer) string {
	if n, ok := c.(interface{ Name() string }); ok {
		return n.Name()
	}
	return fmt.Sprintf("%T", c)
}
