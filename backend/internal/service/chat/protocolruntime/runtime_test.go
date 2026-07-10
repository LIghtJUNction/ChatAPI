package protocolruntime

import (
	"testing"

	"github.com/zyf2007/ChatAPI/internal/protocol"
)

func TestRuntimeBuildsAnthropicBlockState(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol:   protocol.ProtocolAnthropicMessages,
		Model:      "claude-test",
		ResponseID: "msg_test",
	})

	start := runtime.Start()
	if len(start) != 2 || start[0].Event != "message_start" || start[1].Event != "ping" {
		t.Fatalf("unexpected start events: %#v", start)
	}

	first := runtime.Apply(Action{Kind: ActionDelta, DeltaText: "hello"})
	if len(first.StreamEvents) != 2 {
		t.Fatalf("expected content block start plus delta, got %#v", first.StreamEvents)
	}
	if first.StreamEvents[0].Event != "content_block_start" || first.StreamEvents[1].Event != "content_block_delta" {
		t.Fatalf("unexpected first delta events: %#v", first.StreamEvents)
	}

	second := runtime.Apply(Action{Kind: ActionDelta, DeltaText: " world"})
	if len(second.StreamEvents) != 1 || second.StreamEvents[0].Event != "content_block_delta" {
		t.Fatalf("unexpected second delta events: %#v", second.StreamEvents)
	}
}

func TestRuntimeCompletionDoesNotRepeatStreamedText(t *testing.T) {
	tests := []struct {
		name     string
		protocol protocol.Protocol
	}{
		{name: "responses", protocol: protocol.ProtocolResponses},
		{name: "chat_completions", protocol: protocol.ProtocolChatCompletions},
		{name: "anthropic", protocol: protocol.ProtocolAnthropicMessages},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := New(protocol.ConversationMeta{Protocol: test.protocol, Model: "test-model"})
			delta := runtime.Apply(Action{Kind: ActionDelta, DeltaText: "already streamed"})
			completed := runtime.Apply(Action{Kind: ActionComplete})

			for _, event := range completed.StreamEvents {
				if streamEventText(event) == "already streamed" {
					t.Fatalf("completion repeated an earlier delta: %#v; delta events: %#v", completed.StreamEvents, delta.StreamEvents)
				}
			}
		})
	}
}

func streamEventText(event protocol.StreamEvent) string {
	data, ok := event.Data.(map[string]any)
	if !ok {
		return ""
	}
	if delta, ok := data["delta"].(string); ok {
		return delta
	}
	choices, ok := data["choices"].([]map[string]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	delta, _ := choices[0]["delta"].(map[string]any)
	text, _ := delta["content"].(string)
	return text
}

func TestRuntimeBuildsResponsesAbort(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol:   protocol.ProtocolResponses,
		Model:      "gpt-test",
		ResponseID: "resp_test",
	})

	result := runtime.Apply(Action{
		Kind: ActionAbort,
		ErrorBody: map[string]any{
			"error": map[string]any{
				"message": "request disconnected",
				"type":    "request_disconnected",
				"code":    "request_disconnected",
			},
		},
	})
	if len(result.StreamEvents) != 1 || result.StreamEvents[0].Event != "response.failed" {
		t.Fatalf("unexpected abort events: %#v", result.StreamEvents)
	}
}

func TestRuntimeBuildsResponsesToolCallLifecycle(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol:   protocol.ProtocolResponses,
		Model:      "gpt-test",
		ResponseID: "resp_test",
	})

	result := runtime.Apply(Action{
		Kind:       ActionComplete,
		Mode:       "tool_call",
		ToolName:   "lookup_weather",
		ToolCallID: "call_1",
		OutputText: `{"city":"Hangzhou"}`,
	})

	requireEventOrder(t, result.StreamEvents,
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.completed",
	)
}

func TestRuntimeBuildsResponsesBuiltinToolEvents(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol:   protocol.ProtocolResponses,
		Model:      "gpt-test",
		ResponseID: "resp_test",
	})

	search := runtime.Apply(Action{
		Kind:             ActionBuiltin,
		BuiltinToolKind:  "web_search",
		BuiltinToolQuery: "latest go release",
	})
	requireEventOrder(t, search.StreamEvents,
		"response.output_item.added",
		"response.web_search_call.in_progress",
		"response.web_search_call.searching",
		"response.web_search_call.completed",
		"response.output_item.done",
	)

	image := runtime.Apply(Action{
		Kind:              ActionBuiltin,
		BuiltinToolKind:   "image_generation",
		BuiltinToolResult: "aW1hZ2U=",
	})
	requireEventOrder(t, image.StreamEvents,
		"response.output_item.added",
		"response.image_generation_call.in_progress",
		"response.image_generation_call.generating",
		"response.image_generation_call.completed",
		"response.output_item.done",
	)
	done := image.StreamEvents[len(image.StreamEvents)-1].Data.(map[string]any)
	item := done["item"].(map[string]any)
	if item["type"] != "image_generation_call" || item["result"] != "aW1hZ2U=" {
		t.Fatalf("unexpected image generation output item: %#v", item)
	}
}

func TestRuntimeBuildsChatCompletionToolCallChunks(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol: protocol.ProtocolChatCompletions,
		Model:    "gpt-test",
	})

	result := runtime.Apply(Action{
		Kind:       ActionComplete,
		Mode:       "tool_call",
		ToolName:   "lookup_weather",
		ToolCallID: "call_1",
		OutputText: `{"city":"Hangzhou"}`,
	})

	if len(result.StreamEvents) != 3 {
		t.Fatalf("unexpected chat tool events: %#v", result.StreamEvents)
	}
	first := result.StreamEvents[0].Data.(map[string]any)
	choices := first["choices"].([]map[string]any)
	delta := choices[0]["delta"].(map[string]any)
	if _, ok := delta["tool_calls"]; !ok {
		t.Fatalf("missing chat tool_calls delta: %#v", first)
	}
	stop := result.StreamEvents[1].Data.(map[string]any)
	stopChoices := stop["choices"].([]map[string]any)
	if stopChoices[0]["finish_reason"] != "tool_calls" {
		t.Fatalf("unexpected chat finish reason: %#v", stop)
	}
	if result.StreamEvents[2].Data != "[DONE]" {
		t.Fatalf("missing chat done marker: %#v", result.StreamEvents)
	}
}

func TestRuntimeIgnoresAnthropicThinkingWithoutSignedBlock(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol: protocol.ProtocolAnthropicMessages,
		Model:    "claude-test",
	})

	thinking := runtime.Apply(Action{Kind: ActionDelta, Mode: "thinking", DeltaText: "think"})
	if len(thinking.StreamEvents) != 0 {
		t.Fatalf("anthropic thinking without provider signature must not emit official thinking events: %#v", thinking.StreamEvents)
	}

	text := runtime.Apply(Action{Kind: ActionDelta, Mode: "answer", DeltaText: "answer"})
	done := runtime.Apply(Action{Kind: ActionComplete})

	requireEventOrder(t, append(text.StreamEvents, done.StreamEvents...),
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	)
}

func TestRuntimeBuildsAnthropicToolUseLifecycle(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol: protocol.ProtocolAnthropicMessages,
		Model:    "claude-test",
	})

	result := runtime.Apply(Action{
		Kind:       ActionComplete,
		Mode:       "tool_call",
		ToolName:   "lookup_weather",
		ToolCallID: "toolu_1",
		OutputText: `{"city":"Hangzhou"}`,
	})

	requireEventOrder(t, result.StreamEvents,
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	)
	delta := result.StreamEvents[1].Data.(map[string]any)["delta"].(map[string]any)
	if delta["type"] != "input_json_delta" {
		t.Fatalf("unexpected anthropic tool delta: %#v", delta)
	}
}

func requireEventOrder(t *testing.T, events []protocol.StreamEvent, expected ...string) {
	t.Helper()
	if len(events) != len(expected) {
		t.Fatalf("unexpected event count got=%d want=%d events=%#v", len(events), len(expected), events)
	}
	for index, want := range expected {
		if events[index].Event != want {
			t.Fatalf("unexpected event at %d got=%q want=%q events=%#v", index, events[index].Event, want, events)
		}
	}
}
