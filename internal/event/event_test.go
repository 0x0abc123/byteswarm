package event

import "testing"

func TestEventZeroValue(t *testing.T) {
	var e Event
	if e.Type != "" || e.WorkflowID != "" || e.Payload != nil {
		t.Fatalf("zero-value Event should be empty, got %+v", e)
	}
}
