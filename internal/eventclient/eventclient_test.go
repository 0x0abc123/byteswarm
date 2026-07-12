package eventclient

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

// startSocketServer serves h on a Unix domain socket in a temp dir and returns
// the socket path — the same transport the client uses to reach /events.
func startSocketServer(t *testing.T, h http.Handler) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "e.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return sock
}

func TestPublishSendsEvent(t *testing.T) {
	type captured struct {
		method, path string
		body         Event
	}
	got := make(chan captured, 1)
	sock := startSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b Event
		_ = json.NewDecoder(r.Body).Decode(&b)
		got <- captured{r.Method, r.URL.Path, b}
		w.WriteHeader(http.StatusAccepted)
	}))

	err := New(sock).Publish(context.Background(), Event{
		Type: "job_done", WorkflowID: "wf1", Payload: json.RawMessage(`{"id":7}`),
	})
	if err != nil {
		t.Fatalf("Publish error: %v", err)
	}

	c := <-got
	if c.method != http.MethodPost || c.path != "/events" {
		t.Fatalf("request = %s %s, want POST /events", c.method, c.path)
	}
	if c.body.Type != "job_done" || c.body.WorkflowID != "wf1" {
		t.Fatalf("body = %+v, want type=job_done workflowID=wf1", c.body)
	}
	if string(c.body.Payload) != `{"id":7}` {
		t.Fatalf("payload = %s, want %s", c.body.Payload, `{"id":7}`)
	}
}

func TestPublishNon2xxErrors(t *testing.T) {
	sock := startSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	if err := New(sock).Publish(context.Background(), Event{Type: "t"}); err == nil {
		t.Fatal("Publish with a 400 response should return an error")
	}
}

func TestPublishDialError(t *testing.T) {
	// No server listening at this path → dial fails → error, not panic.
	if err := New(filepath.Join(t.TempDir(), "absent.sock")).Publish(context.Background(), Event{Type: "t"}); err == nil {
		t.Fatal("Publish to an absent socket should return an error")
	}
}
