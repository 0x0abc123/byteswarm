package bus

import (
	"context"
	"errors"
	"fmt"
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

func collectReplay(t *testing.T, b *JetStreamBus, subject string, since time.Time) []event.Event {
	t.Helper()
	var got []event.Event
	err := b.Replay(context.Background(), subject, since, func(_ context.Context, e event.Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return got
}

func TestReplayReadsAllFromStart(t *testing.T) {
	b, err := New(Config{URL: startJetStreamServer(t), Stream: "BYTESWARM_REPLAY"}, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	types := []string{"alpha", "beta", "gamma"}
	for i, ty := range types {
		if err := b.Publish(context.Background(), event.Event{
			Type: ty, WorkflowID: "wf1", Payload: []byte(fmt.Sprintf(`{"i":%d}`, i)),
		}); err != nil {
			t.Fatalf("publish %s: %v", ty, err)
		}
	}

	got := collectReplay(t, b, event.SubjectAll, time.Time{})
	if len(got) != len(types) {
		t.Fatalf("replayed %d events, want %d", len(got), len(types))
	}
	for i, ty := range types {
		if got[i].Type != ty {
			t.Fatalf("order: got[%d].Type = %q, want %q", i, got[i].Type, ty)
		}
	}
	if string(got[0].Payload) != `{"i":0}` {
		t.Fatalf("got[0].Payload = %s, want %s", got[0].Payload, `{"i":0}`)
	}
}

func TestReplayDoesNotDisturbLiveDurable(t *testing.T) {
	b, err := New(Config{URL: startJetStreamServer(t), Stream: "BYTESWARM_REPLAY_LIVE", AckWait: 500 * time.Millisecond}, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	live := make(chan event.Event, 8)
	err = b.Subscribe(ctx, event.SubjectAll, func(_ context.Context, e event.Event) error {
		select {
		case live <- e:
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := b.Publish(ctx, event.Event{Type: "live1", WorkflowID: "wf1"}); err != nil {
		t.Fatalf("publish live1: %v", err)
	}
	awaitEventType(t, live, "live1")

	// Replay historical events — must not touch the durable's cursor.
	_ = collectReplay(t, b, event.SubjectAll, time.Time{})

	// The durable keeps delivering new events from where it left off.
	if err := b.Publish(ctx, event.Event{Type: "live2", WorkflowID: "wf1"}); err != nil {
		t.Fatalf("publish live2: %v", err)
	}
	awaitEventType(t, live, "live2")
}

func TestReplaySince(t *testing.T) {
	b, err := New(Config{URL: startJetStreamServer(t), Stream: "BYTESWARM_REPLAY_SINCE"}, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	ctx := context.Background()

	if err := b.Publish(ctx, event.Event{Type: "early", WorkflowID: "wf1"}); err != nil {
		t.Fatalf("publish early: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	mark := time.Now()
	time.Sleep(150 * time.Millisecond)
	for _, ty := range []string{"late1", "late2"} {
		if err := b.Publish(ctx, event.Event{Type: ty, WorkflowID: "wf1"}); err != nil {
			t.Fatalf("publish %s: %v", ty, err)
		}
	}

	got := collectReplay(t, b, event.SubjectAll, mark)
	if len(got) != 2 {
		t.Fatalf("since-replay returned %d events, want 2 (only after mark): %+v", len(got), got)
	}
	if got[0].Type != "late1" || got[1].Type != "late2" {
		t.Fatalf("since-replay = [%q %q], want [late1 late2]", got[0].Type, got[1].Type)
	}
}

func awaitEventType(t *testing.T, ch chan event.Event, wantType string) {
	t.Helper()
	select {
	case e := <-ch:
		if e.Type != wantType {
			t.Fatalf("received event %q, want %q", e.Type, wantType)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timed out waiting for event %q", wantType)
	}
}

func TestScopedSubscriptionFiltersByWorkflow(t *testing.T) {
	b, err := New(Config{URL: startJetStreamServer(t), Stream: "BYTESWARM_SCOPE"}, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan event.Event, 8)
	// Scope to wfA via broker-side subject filtering (single-token type,
	// ADR-0010, makes the '*' pin the workflowID token).
	err = b.Subscribe(ctx, "bw.evt.*.wfA", func(_ context.Context, e event.Event) error {
		select {
		case got <- e:
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := b.Publish(ctx, event.Event{Type: "task", WorkflowID: "wfB"}); err != nil {
		t.Fatalf("publish wfB: %v", err)
	}
	if err := b.Publish(ctx, event.Event{Type: "task", WorkflowID: "wfA"}); err != nil {
		t.Fatalf("publish wfA: %v", err)
	}

	select {
	case e := <-got:
		if e.WorkflowID != "wfA" {
			t.Fatalf("scoped instance received workflow %q, want wfA (broker-side filter leaked)", e.WorkflowID)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("scoped instance did not receive its own workflow's event")
	}
	// wfB's event must not arrive.
	select {
	case e := <-got:
		t.Fatalf("scoped instance unexpectedly received a non-scoped event: %+v", e)
	case <-time.After(500 * time.Millisecond):
	}
}
