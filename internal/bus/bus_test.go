package bus

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	"github.com/0x0abc123/byteswarm/internal/event"
)

// startJetStreamServer runs an in-process JetStream-enabled NATS server on a
// random port so the integration test is hermetic (no external broker). It
// skips cleanly if the embedded server cannot start.
func startJetStreamServer(t *testing.T) string {
	t.Helper()
	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1, // random free port
		JetStream: true,
		StoreDir:  t.TempDir(),
	}
	s, err := natsserver.NewServer(opts)
	if err != nil {
		t.Skipf("cannot create embedded NATS server: %v", err)
	}
	go s.Start()
	if !s.ReadyForConnections(5 * time.Second) {
		s.Shutdown()
		t.Skip("embedded NATS server not ready")
	}
	t.Cleanup(s.Shutdown)
	return s.ClientURL()
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSubjectForEncoding(t *testing.T) {
	got, err := subjectFor(event.Event{Type: "order_created", WorkflowID: "wf1"})
	if err != nil {
		t.Fatalf("subjectFor: %v", err)
	}
	if got != "bw.evt.order_created.wf1" {
		t.Fatalf("subject = %q, want %q", got, "bw.evt.order_created.wf1")
	}

	got, err = subjectFor(event.Event{Type: "ping"})
	if err != nil {
		t.Fatalf("subjectFor(no workflow): %v", err)
	}
	if got != "bw.evt.ping._" {
		t.Fatalf("subject = %q, want %q", got, "bw.evt.ping._")
	}

	for _, bad := range []event.Event{
		{Type: ""},
		{Type: "a>b", WorkflowID: "w"},
		{Type: "ok", WorkflowID: "has space"},
	} {
		if _, err := subjectFor(bad); err == nil {
			t.Fatalf("subjectFor(%+v) = nil error, want rejection", bad)
		}
	}
}

func TestPublishSubscribeRoundTrip(t *testing.T) {
	url := startJetStreamServer(t)
	b, err := New(Config{URL: url, Stream: "BYTESWARM_TEST"}, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan event.Event, 1)
	if err := b.Subscribe(ctx, "bw.evt.order_created.*", func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	want := event.Event{Type: "order_created", WorkflowID: "wf1", Payload: []byte(`{"id":7}`)}
	if err := b.Publish(ctx, want); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case e := <-got:
		if e.Type != want.Type || e.WorkflowID != want.WorkflowID || string(e.Payload) != string(want.Payload) {
			t.Fatalf("received %+v, want %+v", e, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive published event within 5s")
	}
}

func TestBoundedRedelivery(t *testing.T) {
	url := startJetStreamServer(t)
	b, err := New(Config{URL: url, Stream: "BYTESWARM_REDELIVER", AckWait: 150 * time.Millisecond}, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int64
	err = b.Subscribe(ctx, "bw.evt.poison.*", func(_ context.Context, _ event.Event) error {
		calls.Add(1)
		return errors.New("always fails")
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := b.Publish(ctx, event.Event{Type: "poison", WorkflowID: "wf1", Payload: []byte("x")}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// A permanently-failing event is redelivered up to maxDeliver times, then
	// terminated — it must not loop forever.
	// Generous ceiling: the loop exits the instant it reaches maxDeliver, so a
	// large bound only matters under heavy CI/-race load (MaxDeliver makes
	// over-counting impossible; the only failure mode is being too slow).
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && calls.Load() < int64(maxDeliver) {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond) // settle: confirm no further redelivery
	if got := calls.Load(); got != int64(maxDeliver) {
		t.Fatalf("handler invoked %d times, want exactly %d (bounded redelivery)", got, maxDeliver)
	}
}

func TestDurableResumeAfterRestart(t *testing.T) {
	url := startJetStreamServer(t)
	newBus := func() *JetStreamBus {
		b, err := New(Config{URL: url, Stream: "BYTESWARM_RESUME", AckWait: 500 * time.Millisecond}, testLogger())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return b
	}
	send := func(ch chan struct{}) { // non-blocking so the NATS callback never stalls
		select {
		case ch <- struct{}{}:
		default:
		}
	}

	// Instance 1: ack A, leave B unacked (Nak).
	bus1 := newBus()
	ctx1, cancel1 := context.WithCancel(context.Background())
	gotA := make(chan struct{}, 1)
	gotB1 := make(chan struct{}, 1)
	err := bus1.Subscribe(ctx1, "bw.evt.>", func(_ context.Context, e event.Event) error {
		if e.Type == "resumeA" {
			send(gotA)
			return nil // ack A -> cursor advances past it
		}
		send(gotB1)
		return errors.New("leave B unacked") // Nak B
	})
	if err != nil {
		t.Fatalf("Subscribe1: %v", err)
	}
	if err := bus1.Publish(ctx1, event.Event{Type: "resumeA", WorkflowID: "wf1"}); err != nil {
		t.Fatalf("publish A: %v", err)
	}
	if err := bus1.Publish(ctx1, event.Event{Type: "resumeB", WorkflowID: "wf1"}); err != nil {
		t.Fatalf("publish B: %v", err)
	}
	awaitSignal(t, gotA, "instance 1 to ack A")
	awaitSignal(t, gotB1, "instance 1 to receive B")

	cancel1()
	_ = bus1.Close() // durable must persist: B unacked, A acked

	// Instance 2 ("restart"): same durable. Must receive B (unacked) and never
	// A (acked → not replayed); a deleted+recreated durable would replay A too.
	bus2 := newBus()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	defer func() { _ = bus2.Close() }()
	got2 := make(chan event.Event, 4)
	err = bus2.Subscribe(ctx2, "bw.evt.>", func(_ context.Context, e event.Event) error {
		select {
		case got2 <- e:
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe2: %v", err)
	}

	select {
	case e := <-got2:
		if e.Type != "resumeB" {
			t.Fatalf("instance 2 first received %q, want resumeB (A was acked on instance 1 and must not replay)", e.Type)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("instance 2 did not receive the unacked event B — durable did not resume")
	}
}

func awaitSignal(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(8 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}
