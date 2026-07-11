package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestPublishSendsRequest(t *testing.T) {
	type captured struct {
		method, path string
		body         publishBody
	}
	got := make(chan captured, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b publishBody
		_ = json.NewDecoder(r.Body).Decode(&b)
		got <- captured{r.Method, r.URL.Path, b}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ts.Close()

	var out bytes.Buffer
	err := publishCmd([]string{"--type", "order.created", "--workflow", "wf1", "--payload", `{"id":7}`, "--addr", ts.URL}, &out)
	if err != nil {
		t.Fatalf("publishCmd error: %v", err)
	}

	c := <-got
	if c.method != http.MethodPost || c.path != "/events" {
		t.Fatalf("request = %s %s, want POST /events", c.method, c.path)
	}
	if c.body.Type != "order.created" || c.body.WorkflowID != "wf1" {
		t.Fatalf("body = %+v, want type=order.created workflowID=wf1", c.body)
	}
	if string(c.body.Payload) != `{"id":7}` {
		t.Fatalf("payload = %s, want %s", c.body.Payload, `{"id":7}`)
	}
}

func TestPublishNon2xxErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer ts.Close()

	var out bytes.Buffer
	if err := publishCmd([]string{"--type", "t", "--addr", ts.URL}, &out); err == nil {
		t.Fatal("publishCmd with a 400 response should return an error")
	}
}

func TestPublishInvalidPayloadRejectedBeforeSend(t *testing.T) {
	var hits atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ts.Close()

	var out bytes.Buffer
	if err := publishCmd([]string{"--type", "t", "--payload", "{not json", "--addr", ts.URL}, &out); err == nil {
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

func TestNormalizeAddr(t *testing.T) {
	cases := map[string]string{
		"http://x:8080":  "http://x:8080",
		"https://x":      "https://x",
		":8080":          "http://localhost:8080",
		"localhost:8080": "http://localhost:8080",
	}
	for in, want := range cases {
		if got := normalizeAddr(in); got != want {
			t.Errorf("normalizeAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
