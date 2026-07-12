package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func newBufEmitter() (*SlogEmitter, *bytes.Buffer) {
	var buf bytes.Buffer
	return NewSlogEmitter(slog.New(slog.NewJSONHandler(&buf, nil))), &buf
}

func TestSlogEmitterEmitsNameAndAttrs(t *testing.T) {
	e, buf := newBufEmitter()

	if err := e.Emit(context.Background(), "order_processed", map[string]any{
		"order_id": "o1",
		"count":    3,
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		`"msg":"business event"`,
		`"event":"order_processed"`,
		`"order_id":"o1"`,
		`"count":3`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("emit output missing %q\ngot: %s", want, out)
		}
	}
}

func TestSlogEmitterEmptyNameFailsAndEmitsNothing(t *testing.T) {
	e, buf := newBufEmitter()

	if err := e.Emit(context.Background(), "", map[string]any{"x": 1}); !errors.Is(err, ErrEmptyEventName) {
		t.Fatalf("Emit(empty name) error = %v, want ErrEmptyEventName", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("empty-name Emit wrote output: %q", buf.String())
	}
}

func TestSlogEmitterNilAttrs(t *testing.T) {
	e, buf := newBufEmitter()

	if err := e.Emit(context.Background(), "ping", nil); err != nil {
		t.Fatalf("Emit(nil attrs): %v", err)
	}
	if !strings.Contains(buf.String(), `"event":"ping"`) {
		t.Fatalf("output missing event field: %q", buf.String())
	}
}
