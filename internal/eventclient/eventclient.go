// Package eventclient publishes events to a byteswarm server's operator-local
// /events ingress over its Unix domain socket (ADR-0011). It is the shared
// socket-dialing publish client used by byteswarmctl and by the job-runner's
// host.publish capability (ADR-0013), so neither process needs NATS
// credentials — the server owns the bus connection.
package eventclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// eventsURL is a fixed placeholder: the socket-dialing transport ignores the
// host and connects to the configured Unix socket path (ADR-0011).
const eventsURL = "http://unix/events"

// Event is one event submitted to /events. Payload is passed through as raw
// JSON (nil → omitted). It mirrors the server's POST /events contract.
type Event struct {
	Type       string          `json:"type"`
	WorkflowID string          `json:"workflowID,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// Client submits events to one /events socket.
type Client struct {
	http *http.Client
}

// New returns a Client whose transport dials the given Unix socket path for
// every request regardless of request host — the /events ingress is not on the
// network. The HTTP request/response contract is otherwise unchanged.
func New(socket string) *Client {
	return &Client{
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socket)
				},
			},
		},
	}
}

// Publish POSTs one event to /events and reports the outcome. A non-2xx
// response is an error carrying the server's (bounded) message; the server
// re-validates the event at its boundary (ADR-0010).
func (c *Client) Publish(ctx context.Context, e Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("eventclient: encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, eventsURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("eventclient: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("eventclient: sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("eventclient: server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}
