package outputpolicy

import (
	"testing"

	"github.com/zyf2007/ChatAPI/internal/protocol"
)

func TestApplyStopAndMaxOutput(t *testing.T) {
	max := 8
	request := protocol.TurnRequest{
		Options: protocol.TurnOptions{
			Stop:            []string{"END"},
			MaxOutputTokens: &max,
		},
	}

	result := Apply(Input{
		Request:      request,
		ExistingText: "hello",
		Text:         " world END ignored",
	})

	if result.Text != " wo" {
		t.Fatalf("unexpected policy text %q", result.Text)
	}
	if !result.StopHit || result.StopSequence != "END" {
		t.Fatalf("expected stop hit: %#v", result)
	}
	if !result.MaxOutputHit || result.MaxOutputChars != 8 {
		t.Fatalf("expected max output hit: %#v", result)
	}
	if result.Metadata() == nil {
		t.Fatal("expected policy metadata")
	}
}

func TestApplyDoesNotTruncateToolArguments(t *testing.T) {
	max := 4
	request := protocol.TurnRequest{Options: protocol.TurnOptions{MaxOutputTokens: &max}}

	result := Apply(Input{
		Request: request,
		Text:    `{"city":"Hangzhou"}`,
		Mode:    "tool_call",
	})

	if result.Text != `{"city":"Hangzhou"}` || result.MaxOutputHit {
		t.Fatalf("tool call arguments must not be truncated: %#v", result)
	}
}

func TestApplyMarksStoreFalseAsPartiallyApplied(t *testing.T) {
	store := false
	result := Apply(Input{
		Request: protocol.TurnRequest{Options: protocol.TurnOptions{Store: &store}},
		Text:    "hello",
	})

	if !result.StoreFalse || result.StoreApplied {
		t.Fatalf("unexpected store policy: %#v", result)
	}
	metadata := result.Metadata()
	if metadata["store_applied"] != false {
		t.Fatalf("expected store_applied=false metadata: %#v", metadata)
	}
}
