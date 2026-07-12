package protocol

import "strings"

type CompletionOutcome string

const (
	CompletionComplete     CompletionOutcome = "complete"
	CompletionLength       CompletionOutcome = "length"
	CompletionStopSequence CompletionOutcome = "stop_sequence"
	CompletionToolCall     CompletionOutcome = "tool_call"
)

func ResolveCompletionOutcome(finishReason string, mode string) CompletionOutcome {
	switch strings.TrimSpace(finishReason) {
	case "length":
		return CompletionLength
	case "stop_sequence":
		return CompletionStopSequence
	}
	if strings.TrimSpace(mode) == "tool_call" {
		return CompletionToolCall
	}
	return CompletionComplete
}

func (o CompletionOutcome) ChatFinishReason() string {
	switch o {
	case CompletionLength:
		return "length"
	case CompletionToolCall:
		return "tool_calls"
	default:
		return "stop"
	}
}

func (o CompletionOutcome) AnthropicStopReason() string {
	switch o {
	case CompletionLength:
		return "max_tokens"
	case CompletionStopSequence:
		return "stop_sequence"
	case CompletionToolCall:
		return "tool_use"
	default:
		return "end_turn"
	}
}

func (o CompletionOutcome) ResponsesIncomplete() bool {
	return o == CompletionLength
}
