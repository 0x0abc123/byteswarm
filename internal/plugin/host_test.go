package plugin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/0x0abc123/byteswarm/internal/consumer"
	"github.com/0x0abc123/byteswarm/internal/event"
)

func testHost(repo consumer.Repository, pub event.Publisher, root string) *Host {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHost(repo, pub, ExecAllowlist{"backup": {"/usr/bin/tar"}}, root, log)
}

func TestHostLoadWiresCapabilities(t *testing.T) {
	h := testHost(&fakeRepo{}, &fakePublisher{}, t.TempDir())

	sc, err := h.Load(PluginConfig{Name: "greet", Events: []string{"order_created"}, Script: "1"})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if sc.Name() != "greet" {
		t.Fatalf("Name() = %q, want %q", sc.Name(), "greet")
	}
	if got := sc.Events(); len(got) != 1 || got[0] != "order_created" {
		t.Fatalf("Events() = %v, want [order_created]", got)
	}
	if sc.caps.Exec == nil || sc.caps.Store == nil || sc.caps.FS == nil || sc.caps.Publish == nil {
		t.Fatal("Load left a capability unwired")
	}
}

func TestScriptConsumerSatisfiesPort(t *testing.T) {
	var c consumer.Consumer = &ScriptConsumer{name: "x"}
	// A consumer with no compiled program fails closed rather than running.
	if err := c.Handle(context.Background(), event.Event{Type: "t"}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Handle(no program) error = %v, want ErrNotImplemented", err)
	}
}

func TestHandleRunsScriptEndToEnd(t *testing.T) {
	repo := &fakeRepo{}
	pub := &fakePublisher{}
	h := testHost(repo, pub, t.TempDir())

	// Script reads the event, writes namespaced state, and publishes a derived event.
	sc, err := h.Load(PluginConfig{
		Name:   "greet",
		Events: []string{"order_created"},
		Script: `host.store.set("last", event.payload.name);
		         host.publish("greeted", event.workflowID, {greeting: "hi " + event.payload.name});`,
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	ev := event.Event{Type: "order_created", WorkflowID: "wf1", Payload: []byte(`{"name":"ada"}`)}
	if err := sc.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if repo.lastKey != "greet:last" {
		t.Fatalf("store key = %q, want %q (namespaced)", repo.lastKey, "greet:last")
	}
	if len(pub.got) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.got))
	}
	if pub.got[0].Type != "greeted" || pub.got[0].WorkflowID != "wf1" {
		t.Fatalf("published event = %+v, want type=greeted workflowID=wf1", pub.got[0])
	}
	if !strings.Contains(string(pub.got[0].Payload), "hi ada") {
		t.Fatalf("published payload = %s, want it to contain %q", pub.got[0].Payload, "hi ada")
	}
}

func TestHandleExposesPluginHome(t *testing.T) {
	repo := &fakeRepo{}
	pub := &fakePublisher{}
	h := testHost(repo, pub, t.TempDir())

	// A script can learn its sandbox home and report it back.
	sc, err := h.Load(PluginConfig{
		Name:   "greet",
		Events: []string{"x"},
		Script: `host.publish("home_report", event.workflowID, {home: host.fs.home()});`,
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := sc.Handle(context.Background(), event.Event{Type: "x", WorkflowID: "wf1"}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(pub.got) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.got))
	}
	// The reported home is the plugin's sandbox directory (<root>/greet).
	if !strings.Contains(string(pub.got[0].Payload), "greet") {
		t.Fatalf("home payload = %s, want it to contain the plugin dir %q", pub.got[0].Payload, "greet")
	}
}

func TestHandleTimeoutInterruptsRunawayScript(t *testing.T) {
	h := testHost(&fakeRepo{}, &fakePublisher{}, t.TempDir())
	h.Timeout = 100 * time.Millisecond

	sc, err := h.Load(PluginConfig{Name: "loop", Events: []string{"x"}, Script: "while(true){}"})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- sc.Handle(context.Background(), event.Event{Type: "x"}) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Handle(infinite loop) returned nil, want a timeout error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Handle did not return; timeout watchdog failed to interrupt the script")
	}
}

func TestHandleScriptErrorFailsClosed(t *testing.T) {
	h := testHost(&fakeRepo{}, &fakePublisher{}, t.TempDir())
	sc, err := h.Load(PluginConfig{Name: "boom", Events: []string{"x"}, Script: `throw new Error("boom")`})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := sc.Handle(context.Background(), event.Event{Type: "x"}); err == nil {
		t.Fatal("Handle(throwing script) returned nil, want an error (event left unacked)")
	}
}

func TestHandleDeniedExecPropagates(t *testing.T) {
	h := testHost(&fakeRepo{}, &fakePublisher{}, t.TempDir())
	// "rm" is not on the allowlist; the denial must surface as a script error.
	sc, err := h.Load(PluginConfig{Name: "danger", Events: []string{"x"}, Script: `host.exec("rm", ["-rf", "/"])`})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := sc.Handle(context.Background(), event.Event{Type: "x"}); err == nil {
		t.Fatal("Handle(denied exec) returned nil, want an error from the allowlist denial")
	}
}

func TestLoadCompileErrorFailsClosed(t *testing.T) {
	h := testHost(&fakeRepo{}, &fakePublisher{}, t.TempDir())
	if _, err := h.Load(PluginConfig{Name: "bad", Events: []string{"x"}, Script: "function ("}); err == nil {
		t.Fatal("Load(uncompilable script) returned nil error, want compile failure (plugin does not start)")
	}
}

func TestSourcePathEscapeRejected(t *testing.T) {
	h := testHost(&fakeRepo{}, &fakePublisher{}, t.TempDir())
	for _, bad := range []string{"../evil.js", "/etc/passwd"} {
		if _, err := h.Load(PluginConfig{Name: "esc", Events: []string{"x"}, Path: bad}); err == nil {
			t.Fatalf("Load(path=%q) returned nil error, want a path-escape/refusal", bad)
		}
	}
}
