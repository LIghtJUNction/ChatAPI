package protocol

import (
	"testing"

	"github.com/zyf/chatapi/internal/store"
)

func TestParseRequestReturnsTypedProtocol(t *testing.T) {
	request := ParseRequest("chat_completions", map[string]any{
		"model":  "gpt-test",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{map[string]any{"name": "lookup"}},
	})
	if request.Protocol != ProtocolChatCompletions {
		t.Fatalf("unexpected protocol: %q", request.Protocol)
	}
	if request.Model != "gpt-test" || !request.Stream || request.UserContent != "hello" || len(request.ToolSchemas) != 1 {
		t.Fatalf("unexpected turn request: %#v", request)
	}
}

func TestConversationMetaBuildPendingStreamEventsForAnthropic(t *testing.T) {
	conversation := store.Conversation{
		ResponseID: "resp_1",
		Metadata: map[string]any{
			"request_format": "anthropic_messages",
			"model":          "claude-test",
		},
	}
	meta := ConversationMetaFromConversation(conversation)
	events, started := meta.BuildPendingStreamEvents(PendingStreamEvent{
		Type:      "delta",
		DeltaText: "partial",
		Result: TurnResult{
			ResponseID: "resp_1",
			OutputText: "partial",
			Mode:       "assistant_message",
		},
	}, false)
	if !started {
		t.Fatal("expected anthropic block to start")
	}
	if len(events) != 2 || events[0].Event != "content_block_start" || events[1].Event != "content_block_delta" {
		t.Fatalf("unexpected anthropic stream events: %#v", events)
	}
}

func TestBuildResponseToolResultResponses(t *testing.T) {
	conversation := store.Conversation{
		ResponseID: "resp_2",
		Metadata: map[string]any{
			"request_format": "responses",
			"model":          "chatapi-lab",
		},
	}
	body := BuildResponse(conversation, TurnResult{
		ResponseID: "resp_2",
		OutputText: "done",
		Mode:       "tool_result",
		ToolCallID: "call_1",
		ToolOutput: "{\"ok\":true}",
	})
	output, ok := body["output"].([]map[string]any)
	if !ok || len(output) != 1 {
		t.Fatalf("unexpected output payload: %#v", body)
	}
	if output[0]["type"] != "function_call_output" || output[0]["call_id"] != "call_1" {
		t.Fatalf("unexpected tool result output: %#v", output[0])
	}
}
