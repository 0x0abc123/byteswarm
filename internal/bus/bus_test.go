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
