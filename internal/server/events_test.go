package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/0x0abc123/byteswarm/internal/auth"
	"github.com/0x0abc123/byteswarm/internal/event"
)

// fakePublisher records published events and can be told to fail, so handler
// behavior is asserted through the event.Publisher port.
type fakePublisher struct {
	mu   sync.Mutex
	got  []event.Event
	fail bool
}

func (p *fakePublisher) Publish(_ context.Context, e event.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail {
		return errors.New("bus unavailable")
	}
	p.got = append(p.got, e)
	return nil
}
func (p *fakePublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.got)
}

func testHandler(pub event.Publisher) http.Handler {
	return New(slog.New(slog.NewJSONHandler(io.Discard, nil)), pub, auth.NewSharedSecret("test-secret"))
}

func TestSubmitEventValid(t *testing.T) {
	pub := &fakePublisher{}
	h := testHandler(pub)

	body := `{"type":"order_created","workflowID":"wf1","payload":{"id":7}}`
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if pub.count() != 1 {
		t.Fatalf("published %d events, want 1", pub.count())
	}
	got := pub.got[0]
	if got.Type != "order_created" || got.WorkflowID != "wf1" {
		t.Fatalf("published %+v, want type=order_created workflowID=wf1", got)
	}
	if !strings.Contains(string(got.Payload), `"id":7`) {
		t.Fatalf("payload = %s, want it to carry the submitted JSON", got.Payload)
	}
}

func TestSubmitEventRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"malformed json", `{"type":`, http.StatusBadRequest},
		{"missing type", `{"workflowID":"wf1"}`, http.StatusBadRequest},
		{"unknown field", `{"type":"t","extra":1}`, http.StatusBadRequest},
		{"wildcard in type", `{"type":"a.>"}`, http.StatusBadRequest},
		{"whitespace in workflowID", `{"type":"t","workflowID":"a b"}`, http.StatusBadRequest},
		{"trailing json", `{"type":"t"}{"type":"u"}`, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			h := testHandler(pub)
			req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if pub.count() != 0 {
				t.Fatalf("published %d events, want 0 (rejected input must not publish)", pub.count())
			}
		})
	}
}

func TestSubmitEventOversizeBody(t *testing.T) {
	pub := &fakePublisher{}
	h := testHandler(pub)

	big := `{"type":"t","payload":"` + strings.Repeat("A", maxEventBodyBytes+1) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(big))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if pub.count() != 0 {
		t.Fatalf("published %d events, want 0", pub.count())
	}
}

func TestSubmitEventPublisherError(t *testing.T) {
	pub := &fakePublisher{fail: true}
	h := testHandler(pub)

	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(`{"type":"t"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
