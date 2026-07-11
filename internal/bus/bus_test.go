package bus

import (
	"context"
	"io"
	"log/slog"
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
	got, err := subjectFor(event.Event{Type: "order.created", WorkflowID: "wf1"})
	if err != nil {
		t.Fatalf("subjectFor: %v", err)
	}
	if got != "bw.evt.order.created.wf1" {
		t.Fatalf("subject = %q, want %q", got, "bw.evt.order.created.wf1")
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
	if err := b.Subscribe(ctx, "bw.evt.order.created.*", func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	want := event.Event{Type: "order.created", WorkflowID: "wf1", Payload: []byte(`{"id":7}`)}
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
