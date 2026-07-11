package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/0x0abc123/byteswarm/internal/event"
)

func TestExampleConsumerHandleLogsAndSucceeds(t *testing.T) {
	var buf bytes.Buffer
	c := newExampleConsumer(slog.New(slog.NewJSONHandler(&buf, nil)))

	err := c.Handle(context.Background(), event.Event{
		Type:       "demo",
		WorkflowID: "wf1",
		Payload:    []byte(`{"x":1}`),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"type":"demo"`) || !strings.Contains(out, `"workflowID":"wf1"`) {
		t.Fatalf("log = %q, want it to record the event type and workflowID", out)
	}
	if strings.Contains(out, `{"x":1}`) {
		t.Fatalf("log leaked the payload body: %q", out)
	}
}
