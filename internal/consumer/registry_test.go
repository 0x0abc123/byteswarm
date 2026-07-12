package consumer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/0x0abc123/byteswarm/internal/event"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// recordingConsumer records every event it handles (concurrency-safe).
type recordingConsumer struct {
	mu  sync.Mutex
	got []event.Event
}

func (c *recordingConsumer) Handle(_ context.Context, e event.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, e)
	return nil
}
func (c *recordingConsumer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

type panicConsumer struct{}

func (panicConsumer) Handle(context.Context, event.Event) error { panic("boom") }

type errorConsumer struct{ handled bool }

func (c *errorConsumer) Handle(context.Context, event.Event) error {
	c.handled = true
	return errors.New("deliberate failure")
}

// fakeBus captures the subscription so a test can drive deliveries directly.
type fakeBus struct {
	mu         sync.Mutex
	subjects   []string
	handler    func(context.Context, event.Event) error
	subscribed chan struct{}
}

func newFakeBus() *fakeBus { return &fakeBus{subscribed: make(chan struct{}, 8)} }

func (b *fakeBus) Publish(context.Context, event.Event) error { return nil }
func (b *fakeBus) Subscribe(_ context.Context, subject string, handle func(context.Context, event.Event) error) error {
	b.mu.Lock()
	b.subjects = append(b.subjects, subject)
	b.handler = handle
	b.mu.Unlock()
	b.subscribed <- struct{}{}
	return nil
}

func (b *fakeBus) subjectList() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.subjects...)
}

func TestDispatchRoutesByType(t *testing.T) {
	a, b := &recordingConsumer{}, &recordingConsumer{}
	r := NewRegistry(testLogger())
	r.Register(a, "type.a")
	r.Register(b, "type.b")

	r.dispatch(context.Background(), event.Event{Type: "type.a"})

	if a.count() != 1 {
		t.Fatalf("consumer a handled %d, want 1", a.count())
	}
	if b.count() != 0 {
		t.Fatalf("consumer b handled %d, want 0 (not subscribed to type.a)", b.count())
	}
}

func TestDispatchUnknownTypeIsNoop(t *testing.T) {
	a := &recordingConsumer{}
	r := NewRegistry(testLogger())
	r.Register(a, "type.a")

	r.dispatch(context.Background(), event.Event{Type: "type.unknown"}) // must not panic

	if a.count() != 0 {
		t.Fatalf("consumer handled %d, want 0", a.count())
	}
}

func TestDispatchIsolatesPanicAndError(t *testing.T) {
	survivor := &recordingConsumer{}
	errC := &errorConsumer{}
	r := NewRegistry(testLogger())
	// Register the misbehaving consumers first so the survivor runs after them.
	r.Register(panicConsumer{}, "type.a")
	r.Register(errC, "type.a")
	r.Register(survivor, "type.a")

	r.dispatch(context.Background(), event.Event{Type: "type.a"}) // panic must be recovered

	if !errC.handled {
		t.Fatal("erroring consumer was not invoked")
	}
	if survivor.count() != 1 {
		t.Fatalf("survivor handled %d, want 1 (a sibling panic/error must not stop it)", survivor.count())
	}
}

func TestBroadcastReachesAllConsumers(t *testing.T) {
	a, b := &recordingConsumer{}, &recordingConsumer{}
	r := NewRegistry(testLogger())
	r.Register(a, "type.a")
	r.Register(b, "type.b")

	r.dispatch(context.Background(), event.Event{Type: BroadcastType})

	if a.count() != 1 || b.count() != 1 {
		t.Fatalf("broadcast reached a=%d b=%d, want 1 and 1", a.count(), b.count())
	}
}

func TestDispatchReturnsErrorWhenConsumerFails(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(&errorConsumer{}, "type.a")
	if err := r.dispatch(context.Background(), event.Event{Type: "type.a"}); err == nil {
		t.Fatal("dispatch should return an error when a consumer fails (→ redelivery)")
	}
}

func TestDispatchReturnsErrorWhenConsumerPanics(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(panicConsumer{}, "type.a")
	if err := r.dispatch(context.Background(), event.Event{Type: "type.a"}); err == nil {
		t.Fatal("dispatch should return an error when a consumer panics (recovered, counts as failure)")
	}
}

func TestDispatchReturnsNilWhenAllSucceed(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(&recordingConsumer{}, "type.a")
	r.Register(&recordingConsumer{}, "type.a")
	if err := r.dispatch(context.Background(), event.Event{Type: "type.a"}); err != nil {
		t.Fatalf("dispatch should return nil when all consumers succeed, got %v", err)
	}
	// An unknown type has no consumers → no failure.
	if err := r.dispatch(context.Background(), event.Event{Type: "type.unknown"}); err != nil {
		t.Fatalf("dispatch of an unsubscribed type should return nil, got %v", err)
	}
}

func TestRunHandlerReportsFailureForRedelivery(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(&errorConsumer{}, "type.a")

	fb := newFakeBus()
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- r.Run(ctx, fb, "") }()

	<-fb.subscribed
	if err := fb.handler(ctx, event.Event{Type: "type.a"}); err == nil {
		t.Fatal("Run's delivery handler should return an error when a consumer fails, so the bus Naks and redelivers")
	}

	cancel()
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRunScopedSubscribesToWorkflowAndBroadcast(t *testing.T) {
	r := NewRegistry(testLogger())
	fb := newFakeBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = r.Run(ctx, fb, "wfA") }()

	// A scoped instance makes two subscriptions: its workflow + broadcasts.
	<-fb.subscribed
	<-fb.subscribed
	subs := fb.subjectList()
	if len(subs) != 2 {
		t.Fatalf("scoped instance subscribed to %v, want 2 subjects", subs)
	}
	want := map[string]bool{"bw.evt.*.wfA": true, "bw.evt.@broadcast.>": true}
	for _, s := range subs {
		if !want[s] {
			t.Fatalf("unexpected scoped subject %q; got %v", s, subs)
		}
	}
}

func TestRunSubscribesToAllAndDispatches(t *testing.T) {
	rec := &recordingConsumer{}
	r := NewRegistry(testLogger())
	r.Register(rec, "type.a")

	fb := newFakeBus()
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- r.Run(ctx, fb, "") }()

	<-fb.subscribed
	if subs := fb.subjectList(); len(subs) != 1 || subs[0] != event.SubjectAll {
		t.Fatalf("any-scope subscribed to %v, want [%q]", subs, event.SubjectAll)
	}

	if err := fb.handler(ctx, event.Event{Type: "type.a"}); err != nil {
		t.Fatalf("delivery handler returned error: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("consumer handled %d, want 1", rec.count())
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}
