package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
)

// publishBody is the JSON contract accepted by the server's POST /events
// endpoint (F1.3). Payload is passed through as raw JSON.
type publishBody struct {
	Type       string          `json:"type"`
	WorkflowID string          `json:"workflowID,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// publishCmd parses `publish` flags and submits one event to the server.
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

	body := publishBody{Type: *typ, WorkflowID: *workflow}
	if *payload != "" {
		if !json.Valid([]byte(*payload)) {
			return errors.New("publish: --payload must be valid JSON")
		}
		body.Payload = json.RawMessage(*payload)
	}

	return doPublish(context.Background(), socketClient(*socket), body, out)
}

// eventsURL is the request URL for the /events ingress. The host is a fixed
// placeholder: the socket-dialing transport ignores it and connects to the
// configured Unix socket path (ADR-0011).
const eventsURL = "http://unix/events"

// doPublish POSTs the event to the /events ingress and reports the outcome. It
// is the testable core: the caller injects the HTTP client (which carries the
// socket-dialing transport).
func doPublish(ctx context.Context, client *http.Client, body publishBody, out io.Writer) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("publish: encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, eventsURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("publish: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("publish: sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("publish: server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	fmt.Fprintf(out, "accepted: %s\n", body.Type)
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

// socketClient returns an HTTP client whose transport dials the given Unix
// domain socket regardless of request host — the operator-local /events ingress
// is not on the network (ADR-0011). The HTTP request/response contract is
// otherwise unchanged.
func socketClient(path string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", path)
			},
		},
	}
}
