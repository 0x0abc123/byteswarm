package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
		addr     = fs.String("addr", defaultAddr(), "byteswarm server base URL")
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

	return doPublish(context.Background(), http.DefaultClient, normalizeAddr(*addr), body, out)
}

// doPublish POSTs the event to <addr>/events and reports the outcome. It is the
// testable core: the caller injects the HTTP client and base URL.
func doPublish(ctx context.Context, client *http.Client, addr string, body publishBody, out io.Writer) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("publish: encoding request: %w", err)
	}
	url := strings.TrimRight(addr, "/") + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
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

// defaultAddr resolves the server base URL from BYTESWARM_HTTP_ADDR, falling
// back to localhost. The value is normalized (scheme added) by normalizeAddr.
func defaultAddr() string {
	if a := os.Getenv("BYTESWARM_HTTP_ADDR"); a != "" {
		return a
	}
	return "http://localhost:8080"
}

// normalizeAddr turns a listen-style or bare address into a client base URL, so
// the same BYTESWARM_HTTP_ADDR value the server binds to (e.g. ":8080") also
// works as a CLI target.
func normalizeAddr(a string) string {
	switch {
	case strings.Contains(a, "://"):
		return a
	case strings.HasPrefix(a, ":"):
		return "http://localhost" + a
	default:
		return "http://" + a
	}
}
