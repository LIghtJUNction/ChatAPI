package outputpolicy

import (
	"testing"

	"github.com/zyf2007/ChatAPI/internal/protocol"
)

type runeCounter struct{}

func (runeCounter) Count(text string) (int, error) { return len([]rune(text)), nil }
func (runeCounter) Name() string                   { return "test_runes" }

func newTestGuard(limit int, stops ...string) *Guard {
	return &Guard{
		stops:      normalizedStops(stops),
		tokenLimit: limit,
		capability: TokenCapability{Counter: runeCounter{}, Accuracy: TokenCountExact},
	}
}

func TestGuardAppliesTokenBudgetAcrossChunks(t *testing.T) {
	guard := newTestGuard(8)
	first, err := guard.Execute(Input{Text: "hello"}, nil)
	if err != nil || first.Text != "hello" || first.Terminal {
		t.Fatalf("unexpected first decision: %#v err=%v", first, err)
	}
	second, err := guard.Execute(Input{Text: " world"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Text != " wo" || second.OutputText != "hello wo" || !second.Terminal || second.FinishReason != "length" {
		t.Fatalf("unexpected budget decision: %#v", second)
	}
}

func TestGuardMatchesStopAcrossChunkBoundary(t *testing.T) {
	guard := newTestGuard(0, "END")
	first, err := guard.Execute(Input{Text: "value E"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Text != "value " || first.OutputText != "value " {
		t.Fatalf("stop prefix must be withheld: %#v", first)
	}
	decision, err := guard.Execute(Input{Text: "ND ignored"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Text != "" || decision.OutputText != "value " || !decision.Terminal || decision.StopSequence != "END" {
		t.Fatalf("unexpected stop decision: %#v", decision)
	}
}

func TestGuardFlushesUnmatchedStopPrefixOnFinal(t *testing.T) {
	guard := newTestGuard(0, "END")
	first, err := guard.Execute(Input{Text: "value E"}, nil)
	if err != nil || first.Text != "value " {
		t.Fatalf("unexpected first decision: %#v err=%v", first, err)
	}
	final, err := guard.Execute(Input{Final: true}, nil)
	if err != nil || final.Text != "E" || final.OutputText != "value E" {
		t.Fatalf("unexpected final decision: %#v err=%v", final, err)
	}
}

func TestGuardDoesNotChargeWithheldStopPrefix(t *testing.T) {
	guard := newTestGuard(1, "END")
	first, err := guard.Execute(Input{Text: "E"}, nil)
	if err != nil || first.Terminal || first.OutputTokens != 0 {
		t.Fatalf("withheld stop prefix consumed budget: %#v err=%v", first, err)
	}
	second, err := guard.Execute(Input{Text: "ND"}, nil)
	if err != nil || !second.Terminal || second.FinishReason != "stop_sequence" || second.OutputTokens != 0 {
		t.Fatalf("unexpected stop completion: %#v err=%v", second, err)
	}
}

func TestGuardWithholdsUnicodeStopPrefixOnRuneBoundary(t *testing.T) {
	guard := newTestGuard(0, "结束")
	first, err := guard.Execute(Input{Text: "回答结"}, nil)
	if err != nil || first.Text != "回答" {
		t.Fatalf("unexpected unicode prefix decision: %#v err=%v", first, err)
	}
	second, err := guard.Execute(Input{Text: "束以后"}, nil)
	if err != nil || second.OutputText != "回答" || second.StopSequence != "结束" {
		t.Fatalf("unexpected unicode stop decision: %#v err=%v", second, err)
	}
}

func TestGuardDoesNotCommitWhenPersistenceFails(t *testing.T) {
	guard := newTestGuard(10)
	_, err := guard.Execute(Input{Text: "failed"}, func(Decision) error { return errTestPersistence })
	if err == nil {
		t.Fatal("expected persistence error")
	}
	decision, err := guard.Execute(Input{Text: "ok"}, nil)
	if err != nil || decision.OutputText != "ok" {
		t.Fatalf("guard advanced after failed persistence: %#v err=%v", decision, err)
	}
}

func TestNewGuardUsesExplicitEstimatedCapabilityForUnknownModel(t *testing.T) {
	max := 4
	guard, err := NewGuard(protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses,
		Model:    "virtual-model",
		Options:  protocol.TurnOptions{MaxOutputTokens: &max},
	})
	if err != nil {
		t.Fatal(err)
	}
	if guard.capability.Counter == nil || guard.capability.Accuracy != TokenCountEstimated {
		t.Fatalf("unexpected capability: %#v", guard.capability)
	}
}

func TestNewGuardCountsOutputWithoutExplicitLimit(t *testing.T) {
	guard, err := NewGuard(protocol.TurnRequest{Protocol: protocol.ProtocolResponses, Model: "virtual-model"})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := guard.Execute(Input{Text: "hello"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.OutputTokens <= 0 || decision.TokenCounter == "" || decision.Metadata()["output_tokens"] == nil {
		t.Fatalf("missing token capability metadata: %#v", decision)
	}
}

var errTestPersistence = testError("persist failed")

type testError string

func (e testError) Error() string { return string(e) }
