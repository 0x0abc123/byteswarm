package event

import (
	"strings"
	"testing"
)

func TestEventZeroValue(t *testing.T) {
	var e Event
	if e.Type != "" || e.WorkflowID != "" || e.Payload != nil {
		t.Fatalf("zero-value Event should be empty, got %+v", e)
	}
}

func TestValidType(t *testing.T) {
	valid := []string{
		"demo",
		"order_created",
		"order-created",
		"Type123",
		BroadcastType, // reserved sentinel is exempt from the charset
	}
	for _, ty := range valid {
		if !ValidType(ty) {
			t.Errorf("ValidType(%q) = false, want true", ty)
		}
	}

	invalid := []string{
		"",              // empty
		"order.created", // dot — the whole point of ADR-0010
		"has space",
		"wild*",       // NATS wildcard
		"deep>",       // NATS wildcard
		"bad@type",    // '@' only allowed as the exact BroadcastType sentinel
		"@broadcastx", // not the exact sentinel
	}
	for _, ty := range invalid {
		if ValidType(ty) {
			t.Errorf("ValidType(%q) = true, want false", ty)
		}
	}

	// Over-length is rejected.
	if ValidType(strings.Repeat("a", maxTypeLen+1)) {
		t.Errorf("ValidType(len %d) = true, want false", maxTypeLen+1)
	}
	if !ValidType(strings.Repeat("a", maxTypeLen)) {
		t.Errorf("ValidType(len %d) = false, want true", maxTypeLen)
	}
}
