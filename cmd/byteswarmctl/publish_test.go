package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// startSocketServer serves h on a Unix domain socket in a temp dir and returns
// the socket path — the same transport byteswarmctl uses to reach /events
// (ADR-0011).
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

func TestPublishSendsRequest(t *testing.T) {
	type captured struct {
		method, path string
		body         publishBody
	}
	got := make(chan captured, 1)
	sock := startSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b publishBody
		_ = json.NewDecoder(r.Body).Decode(&b)
		got <- captured{r.Method, r.URL.Path, b}
		w.WriteHeader(http.StatusAccepted)
	}))

	var out bytes.Buffer
	err := publishCmd([]string{"--type", "order_created", "--workflow", "wf1", "--payload", `{"id":7}`, "--socket", sock}, &out)
	if err != nil {
		t.Fatalf("publishCmd error: %v", err)
	}

	c := <-got
	if c.method != http.MethodPost || c.path != "/events" {
		t.Fatalf("request = %s %s, want POST /events", c.method, c.path)
	}
	if c.body.Type != "order_created" || c.body.WorkflowID != "wf1" {
		t.Fatalf("body = %+v, want type=order_created workflowID=wf1", c.body)
	}
	if string(c.body.Payload) != `{"id":7}` {
		t.Fatalf("payload = %s, want %s", c.body.Payload, `{"id":7}`)
	}
}

func TestPublishNon2xxErrors(t *testing.T) {
	sock := startSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))

	var out bytes.Buffer
	if err := publishCmd([]string{"--type", "t", "--socket", sock}, &out); err == nil {
		t.Fatal("publishCmd with a 400 response should return an error")
	}
}

func TestPublishInvalidPayloadRejectedBeforeSend(t *testing.T) {
	var hits atomic.Int64
	sock := startSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))

	var out bytes.Buffer
	if err := publishCmd([]string{"--type", "t", "--payload", "{not json", "--socket", sock}, &out); err == nil {
		t.Fatal("publishCmd with invalid --payload should return an error")
	}
	if hits.Load() != 0 {
		t.Fatalf("server hit %d times, want 0 (invalid payload must be rejected client-side)", hits.Load())
	}
}

func TestPublishRequiresType(t *testing.T) {
	var out bytes.Buffer
	if err := publishCmd([]string{"--workflow", "w"}, &out); err == nil {
		t.Fatal("publishCmd without --type should return an error")
	}
}

func TestDefaultSocketPath(t *testing.T) {
	t.Setenv("BYTESWARM_EVENTS_SOCKET", "")
	if got := defaultSocketPath(); got != "byteswarm-events.sock" {
		t.Errorf("default = %q, want byteswarm-events.sock", got)
	}
	t.Setenv("BYTESWARM_EVENTS_SOCKET", "/run/byteswarm/e.sock")
	if got := defaultSocketPath(); got != "/run/byteswarm/e.sock" {
		t.Errorf("env override = %q, want /run/byteswarm/e.sock", got)
	}
}
