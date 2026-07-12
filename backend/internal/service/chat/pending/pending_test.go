package pending

import (
	"testing"
	"time"
)

func TestPendingRegistryAllowsMultipleDeltasBeforeComplete(t *testing.T) {
	registry := NewPendingRegistry()
	registry.Add(&PendingTurn{
		ConversationID: "conv_streaming",
		RequestID:      "req_streaming",
		OwnerID:        "user_streaming",
		CreatedAt:      time.Now().UTC(),
		Events:         make(chan PendingEvent, 4),
		Done:           make(chan PendingResult, 1),
	})

	previous, err := registry.StartDelta("conv_streaming")
	if err != nil {
		t.Fatalf("first delta should start: %v", err)
	}
	if previous != "pending" {
		t.Fatalf("unexpected first previous state: %q", previous)
	}

	previous, err = registry.StartDelta("conv_streaming")
	if err != nil {
		t.Fatalf("second delta should continue: %v", err)
	}
	if previous != "streaming" {
		t.Fatalf("unexpected second previous state: %q", previous)
	}

	previous, err = registry.StartComplete("conv_streaming")
	if err != nil {
		t.Fatalf("complete after streaming should start: %v", err)
	}
	if previous != "streaming" {
		t.Fatalf("unexpected complete previous state: %q", previous)
	}
}
