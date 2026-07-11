package plugin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/0x0abc123/byteswarm/internal/consumer"
	"github.com/0x0abc123/byteswarm/internal/event"
)

func TestHostNewConsumerWiresCapabilities(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHost(&fakeRepo{}, &fakePublisher{}, ExecAllowlist{"backup": {"/usr/bin/tar"}}, "/srv/plugins", log)

	sc := h.NewConsumer(PluginConfig{Name: "greet", Events: []string{"order.created"}, Path: "greet.js"})

	if sc.Name() != "greet" {
		t.Fatalf("Name() = %q, want %q", sc.Name(), "greet")
	}
	if got := sc.Events(); len(got) != 1 || got[0] != "order.created" {
		t.Fatalf("Events() = %v, want [order.created]", got)
	}
	if sc.caps.Exec == nil || sc.caps.Store == nil || sc.caps.FS == nil || sc.caps.Publish == nil {
		t.Fatal("NewConsumer left a capability unwired")
	}
}

func TestScriptConsumerSatisfiesPort(t *testing.T) {
	var c consumer.Consumer = &ScriptConsumer{name: "x"}
	// Handle fails closed until the goja runtime is wired.
	if err := c.Handle(context.Background(), event.Event{Type: "t"}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Handle error = %v, want ErrNotImplemented", err)
	}
}
