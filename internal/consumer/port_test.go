package consumer

import (
	"context"
	"testing"

	"github.com/0x0abc123/byteswarm/internal/event"
)

// nopConsumer is a trivial fake proving the Consumer port is satisfiable and
// dispatchable through its interface.
type nopConsumer struct{ handled int }

func (c *nopConsumer) Handle(context.Context, event.Event) error {
	c.handled++
	return nil
}

func TestConsumerInterfaceSatisfied(t *testing.T) {
	c := &nopConsumer{}
	var port Consumer = c
	if err := port.Handle(context.Background(), event.Event{Type: "test"}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if c.handled != 1 {
		t.Fatalf("handled = %d, want 1", c.handled)
	}
}
