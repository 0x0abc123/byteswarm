// Package bus is the NATS JetStream adapter for the internal/event.Bus port
// (ADR-0004). It is an outbound adapter: it depends on the event domain
// package and is wired from the composition root; the domain never imports it,
// and no NATS type crosses the port boundary.
//
// Scope (F1.1): connect, ensure the stream, publish, and deliver to a durable
// subscription with simple ack-on-success. Explicit ack/redelivery and
// durable-cursor recovery are F4.1/F4.2; workflowID subscription scoping is
// F4.4; an in-memory adapter is separate.
package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/0x0abc123/byteswarm/internal/event"
)

const (
	// emptyWorkflowToken stands in for an unset WorkflowID (broadcast events)
	// so a subject always has a well-formed final token.
	emptyWorkflowToken = "_"
	// defaultStream is the JetStream stream name when Config.Stream is unset.
	defaultStream = "BYTESWARM"
	// defaultAckWait is the redelivery interval when Config.AckWait is unset.
	defaultAckWait = 30 * time.Second
	// maxDeliver bounds redelivery so a permanently-failing ("poison") event
	// cannot loop forever; on the final failed attempt it is terminated.
	maxDeliver = 5

	maxTokenLen = 256
)

// Config configures the JetStream connection and stream. Secrets (credentials,
// TLS) arrive via the environment at the composition root and are passed as
// Options — never hard-coded here (reference/security-fundamentals.md).
type Config struct {
	URL     string        // NATS server URL, e.g. nats://host:4222 (from env)
	Stream  string        // JetStream stream name; defaults to "BYTESWARM"
	Name    string        // client connection name (observability)
	Timeout time.Duration // connect timeout; defaults to 5s
	AckWait time.Duration // redelivery interval for unacked messages; defaults to 30s
	Options []nats.Option // TLS/creds and other options wired by the composition root
}

// JetStreamBus implements event.Bus over NATS JetStream.
type JetStreamBus struct {
	nc      *nats.Conn
	js      nats.JetStreamContext
	stream  string
	ackWait time.Duration
	log     *slog.Logger
}

// compile-time proof the adapter satisfies the domain port.
var _ event.Bus = (*JetStreamBus)(nil)

// New connects to NATS, obtains a JetStream context, and ensures the byteswarm
// stream exists (subjects bw.evt.>). The caller owns Close.
func New(cfg Config, log *slog.Logger) (*JetStreamBus, error) {
	if cfg.URL == "" {
		return nil, errors.New("bus: NATS URL is required")
	}
	if log == nil {
		return nil, errors.New("bus: logger is required")
	}
	stream := cfg.Stream
	if stream == "" {
		stream = defaultStream
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	ackWait := cfg.AckWait
	if ackWait == 0 {
		ackWait = defaultAckWait
	}

	opts := append([]nats.Option{
		nats.Name(cfg.Name),
		nats.Timeout(timeout),
		// Survive NATS blips and a broker restart: keep retrying the initial
		// connect and reconnect indefinitely with backoff+jitter (ADR-0004
		// resilience). Durable consumers resume from their stored cursor on
		// reconnect, so unacked events are redelivered.
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Warn("bus: disconnected from NATS", slog.String("err", err.Error()))
			} else {
				log.Info("bus: disconnected from NATS")
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Info("bus: reconnected to NATS", slog.String("url", nc.ConnectedUrl()))
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			log.Warn("bus: NATS connection closed")
		}),
	}, cfg.Options...)
	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("bus: connecting to NATS: %w", err)
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bus: JetStream context: %w", err)
	}
	if err := ensureStream(js, stream); err != nil {
		nc.Close()
		return nil, err
	}
	return &JetStreamBus{nc: nc, js: js, stream: stream, ackWait: ackWait, log: log}, nil
}

// ensureStream creates the stream if it does not already exist.
func ensureStream(js nats.JetStreamContext, stream string) error {
	_, err := js.StreamInfo(stream)
	if err == nil {
		return nil
	}
	if !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("bus: stream info: %w", err)
	}
	if _, err := js.AddStream(&nats.StreamConfig{
		Name:     stream,
		Subjects: []string{event.SubjectAll},
	}); err != nil {
		return fmt.Errorf("bus: creating stream %q: %w", stream, err)
	}
	return nil
}

// Publish maps an Event to its subject and publishes the JSON-encoded event to
// the stream. The message body carries the authoritative fields; the subject
// exists for broker-side routing.
func (b *JetStreamBus) Publish(ctx context.Context, e event.Event) error {
	subject, err := subjectFor(e)
	if err != nil {
		return err
	}
	data, err := json.Marshal(wireEvent{Type: e.Type, WorkflowID: e.WorkflowID, Payload: e.Payload})
	if err != nil {
		return fmt.Errorf("bus: encoding event: %w", err)
	}
	if _, err := b.js.Publish(subject, data, nats.Context(ctx)); err != nil {
		return fmt.Errorf("bus: publishing to %q: %w", subject, err)
	}
	return nil
}

// Subscribe binds a durable JetStream consumer to subject and invokes handle
// for each delivered event until ctx is cancelled. M1 semantics: ack on a
// successful handle, Nak on handler error (redelivery), Term on an undecodable
// message. Proper at-least-once cursor recovery is F4.1/F4.2.
func (b *JetStreamBus) Subscribe(ctx context.Context, subject string, handle func(context.Context, event.Event) error) error {
	if subject == "" {
		return errors.New("bus: subscribe subject is required")
	}
	sub, err := b.js.Subscribe(subject, func(msg *nats.Msg) {
		var w wireEvent
		if err := json.Unmarshal(msg.Data, &w); err != nil {
			b.log.Error("bus: dropping undecodable message", "subject", msg.Subject, "err", err)
			_ = msg.Term() // poison message: do not redeliver
			return
		}
		e := event.Event{Type: w.Type, WorkflowID: w.WorkflowID, Payload: w.Payload}
		if err := handle(ctx, e); err != nil {
			// Bound redelivery: terminate a poison event on its final attempt
			// rather than let it loop forever. Logged, never silently dropped.
			if meta, mErr := msg.Metadata(); mErr == nil && meta.NumDelivered >= maxDeliver {
				b.log.Error("bus: giving up on event after max deliveries",
					"subject", msg.Subject, "deliveries", meta.NumDelivered, "err", err)
				_ = msg.Term()
				return
			}
			b.log.Error("bus: handler failed; will redeliver", "subject", msg.Subject, "err", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	}, nats.Durable(durableName(subject)), nats.ManualAck(), nats.DeliverAll(),
		nats.MaxDeliver(maxDeliver), nats.AckWait(b.ackWait))
	if err != nil {
		return fmt.Errorf("bus: subscribing to %q: %w", subject, err)
	}
	// Deliberately do NOT Unsubscribe on ctx cancellation: for a durable
	// consumer, Unsubscribe (and Drain) delete the server-side durable, which
	// would discard the cursor and prevent resume after a restart (F4.2).
	// Delivery stops when the connection closes; the durable persists so
	// unacked events are redelivered to the next instance.
	_ = sub
	return nil
}

// Close disconnects from NATS. It uses a hard close rather than Drain: draining
// unsubscribes, which deletes durable consumers and would discard their cursors
// (F4.2 needs them to persist for resume). Unacked events are redelivered to
// the next instance from the durable; consumers are idempotent.
func (b *JetStreamBus) Close() error {
	b.nc.Close()
	return nil
}

// Replay reads historical events back from the stream for audit/replay,
// calling handle for each matching event in stream order, from `since` (zero
// time = from the start) up to the stream's last sequence at call time. It is
// read-only and bounded: it uses an ephemeral, ack-none consumer, so it neither
// creates nor advances any durable consumer's cursor and never republishes. It
// does NOT follow live — it stops once caught up to the snapshot, when handle
// returns an error, or when ctx is cancelled. `subject` filters what to read
// (e.g. event.SubjectAll for everything, or bw.evt.*.<workflowID> for one
// workflow).
func (b *JetStreamBus) Replay(ctx context.Context, subject string, since time.Time, handle func(context.Context, event.Event) error) error {
	if subject == "" {
		return errors.New("bus: replay subject is required")
	}
	info, err := b.js.StreamInfo(b.stream)
	if err != nil {
		return fmt.Errorf("bus: replay stream info: %w", err)
	}
	lastSeq := info.State.LastSeq
	if lastSeq == 0 {
		return nil // empty stream — nothing to replay
	}

	start := []nats.SubOpt{nats.AckNone()}
	if since.IsZero() {
		start = append(start, nats.DeliverAll())
	} else {
		start = append(start, nats.StartTime(since))
	}
	// Ephemeral (no Durable) sync subscription: isolated from live durables,
	// auto-removed on Unsubscribe.
	sub, err := b.js.SubscribeSync(subject, start...)
	if err != nil {
		return fmt.Errorf("bus: replay subscribe: %w", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg, err := sub.NextMsg(2 * time.Second)
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				return nil // caught up — no more historical messages
			}
			return fmt.Errorf("bus: replay next message: %w", err)
		}

		var w wireEvent
		if uErr := json.Unmarshal(msg.Data, &w); uErr != nil {
			b.log.Error("bus: replay skipping undecodable message", "subject", msg.Subject, "err", uErr)
		} else if hErr := handle(ctx, event.Event{Type: w.Type, WorkflowID: w.WorkflowID, Payload: w.Payload}); hErr != nil {
			return hErr
		}

		// Stop at the snapshot boundary so replay is bounded (does not follow
		// events published after the call began).
		if meta, mErr := msg.Metadata(); mErr == nil && meta.Sequence.Stream >= lastSeq {
			return nil
		}
	}
}

// wireEvent is the on-the-wire JSON envelope. A []byte Payload marshals as a
// base64 string, so arbitrary payloads round-trip losslessly.
type wireEvent struct {
	Type       string `json:"type"`
	WorkflowID string `json:"workflowID"`
	Payload    []byte `json:"payload,omitempty"`
}

// subjectFor builds bw.evt.<type>.<workflowID>. Type is a single dot-free
// token (event.ValidType, ADR-0010) so it occupies exactly one subject
// position; WorkflowID falls back to "_" when unset and must not contain NATS
// wildcards or whitespace (defense in depth even though ingress validates).
func subjectFor(e event.Event) (string, error) {
	if !event.ValidType(e.Type) {
		return "", fmt.Errorf("bus: invalid event type %q", e.Type)
	}
	wf := e.WorkflowID
	if wf == "" {
		wf = emptyWorkflowToken
	}
	if err := checkToken(wf); err != nil {
		return "", err
	}
	return event.SubjectPrefix + "." + e.Type + "." + wf, nil
}

func checkToken(s string) error {
	if len(s) == 0 || len(s) > maxTokenLen {
		return fmt.Errorf("bus: invalid subject token %q: length", s)
	}
	if strings.ContainsAny(s, " \t\r\n*>") {
		return fmt.Errorf("bus: invalid subject token %q: illegal character", s)
	}
	return nil
}

// durableName derives a stable, subject-safe durable consumer name (NATS
// durable names may not contain '.', '*', '>', or whitespace).
func durableName(subject string) string {
	repl := strings.NewReplacer(".", "_", "*", "all", ">", "gt", "@", "at")
	return "bw_" + repl.Replace(subject)
}
