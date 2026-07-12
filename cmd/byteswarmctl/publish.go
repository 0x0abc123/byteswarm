package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/0x0abc123/byteswarm/internal/eventclient"
)

// publishCmd parses `publish` flags and submits one event to the server's
// operator-local /events ingress over its Unix domain socket (ADR-0011), via
// the shared eventclient.
func publishCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		typ      = fs.String("type", "", "event type (required)")
		workflow = fs.String("workflow", "", "workflowID (optional)")
		payload  = fs.String("payload", "", "event payload as JSON (optional)")
		socket   = fs.String("socket", defaultSocketPath(), "path to the byteswarm /events Unix domain socket")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *typ == "" {
		return errors.New("publish: --type is required")
	}

	evt := eventclient.Event{Type: *typ, WorkflowID: *workflow}
	if *payload != "" {
		if !json.Valid([]byte(*payload)) {
			return errors.New("publish: --payload must be valid JSON")
		}
		evt.Payload = json.RawMessage(*payload)
	}

	if err := eventclient.New(*socket).Publish(context.Background(), evt); err != nil {
		return err
	}
	fmt.Fprintf(out, "accepted: %s\n", *typ)
	return nil
}

// defaultSocketPath resolves the /events socket path from BYTESWARM_EVENTS_SOCKET,
// falling back to a path relative to the working directory that matches the
// server's default (ADR-0011). Production operators point both at an absolute
// path via config/env.
func defaultSocketPath() string {
	if p := os.Getenv("BYTESWARM_EVENTS_SOCKET"); p != "" {
		return p
	}
	return "byteswarm-events.sock"
}
