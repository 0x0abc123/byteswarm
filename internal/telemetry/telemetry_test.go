package telemetry

import (
	"context"
	"testing"
)

// recorder is a fake Emitter proving the port is satisfiable and captures what
// it is asked to emit.
type recorder struct{ last string }

func (r *recorder) Emit(_ context.Context, name string, _ map[string]any) error {
	r.last = name
	return nil
}

func TestEmitterInterfaceSatisfied(t *testing.T) {
	var e Emitter = &recorder{}
	if err := e.Emit(context.Background(), "workflow.started", nil); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}
}
