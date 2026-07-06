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
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "hello"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/a.png"}},
			}},
		},
		"tools": []any{map[string]any{"name": "lookup"}},
		"tool_choice": map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": "lookup",
			},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "structured_answer",
				"schema": map[string]any{
					"type": "object",
				},
			},
		},
	})
	if request.Protocol != ProtocolChatCompletions {
		t.Fatalf("unexpected protocol: %q", request.Protocol)
	}
	if request.Model != "gpt-test" || !request.Stream || request.UserContent != "hello" || len(request.ToolSchemas) != 1 {
		t.Fatalf("unexpected turn request: %#v", request)
	}
	if len(request.InputParts) != 2 || request.InputParts[1].Type != "image" {
		t.Fatalf("unexpected input parts: %#v", request.InputParts)
	}
	if request.ToolChoice.Type != "function" || request.ToolChoice.Name != "lookup" {
		t.Fatalf("unexpected tool choice: %#v", request.ToolChoice)
	}
	if request.ResponseFormat.Type != "json_schema" || request.ResponseFormat.Name != "structured_answer" {
		t.Fatalf("unexpected response format: %#v", request.ResponseFormat)
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
	usage, ok := body["usage"].(map[string]any)
	if !ok || usage["total_tokens"] != 0 {
		t.Fatalf("unexpected usage payload: %#v", body)
	}
}

func TestBuildResponseIncludesUsageAcrossProtocols(t *testing.T) {
	cases := []struct {
		name           string
		requestFormat  string
		result         TurnResult
		usageKey       string
		expectedTokens int
	}{
		{
			name:          "responses",
			requestFormat: "responses",
			result: TurnResult{
				ResponseID: "resp_usage_1",
				OutputText: "done",
				Usage:      Usage{InputTokens: 3, OutputTokens: 5},
			},
			usageKey:       "total_tokens",
			expectedTokens: 8,
		},
		{
			name:          "chat_completions",
			requestFormat: "chat_completions",
			result: TurnResult{
				ResponseID: "resp_usage_2",
				OutputText: "done",
				Usage:      Usage{InputTokens: 2, OutputTokens: 7},
			},
			usageKey:       "total_tokens",
			expectedTokens: 9,
		},
		{
			name:          "anthropic_messages",
			requestFormat: "anthropic_messages",
			result: TurnResult{
				ResponseID: "resp_usage_3",
				OutputText: "done",
				Usage:      Usage{InputTokens: 4, OutputTokens: 6},
			},
			usageKey:       "output_tokens",
			expectedTokens: 6,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conversation := store.Conversation{
				ResponseID: tc.result.ResponseID,
				Metadata: map[string]any{
					"request_format": tc.requestFormat,
					"model":          "test-model",
				},
			}
			body := BuildResponse(conversation, tc.result)
			usage, ok := body["usage"].(map[string]any)
			if !ok {
				t.Fatalf("missing usage payload: %#v", body)
			}
			if got, ok := usage[tc.usageKey].(int); !ok || got != tc.expectedTokens {
				t.Fatalf("unexpected usage %s=%#v payload=%#v", tc.usageKey, usage[tc.usageKey], usage)
			}
		})
	}
}

func TestBuildStreamCompleteIncludesUsageForResponsesAndAnthropic(t *testing.T) {
	responsesEvents := BuildStreamComplete(ConversationMeta{
		Protocol:   ProtocolResponses,
		Model:      "test-model",
		ResponseID: "resp_stream_usage",
	}, TurnResult{
		ResponseID: "resp_stream_usage",
		OutputText: "done",
		Usage:      Usage{InputTokens: 1, OutputTokens: 2},
	})
	responseData := responsesEvents[0].Data.(map[string]any)
	responsePayload := responseData["response"].(map[string]any)
	responseUsage := responsePayload["usage"].(map[string]any)
	if responseUsage["total_tokens"] != 3 {
		t.Fatalf("unexpected responses stream usage: %#v", responseUsage)
	}

	anthropicEvents := BuildStreamComplete(ConversationMeta{
		Protocol:   ProtocolAnthropicMessages,
		Model:      "claude-test",
		ResponseID: "msg_stream_usage",
	}, TurnResult{
		ResponseID: "msg_stream_usage",
		OutputText: "done",
		Usage:      Usage{InputTokens: 2, OutputTokens: 4},
	})
	if len(anthropicEvents) < 2 {
		t.Fatalf("unexpected anthropic stream events: %#v", anthropicEvents)
	}
	messageDelta := anthropicEvents[1].Data.(map[string]any)
	usage := messageDelta["usage"].(map[string]any)
	if usage["output_tokens"] != 4 {
		t.Fatalf("unexpected anthropic stream usage: %#v", usage)
	}
}

func TestValidateRequestRejectsEmptyInput(t *testing.T) {
	cases := []struct {
		name         string
		protocolName string
		body         map[string]any
		param        string
	}{
		{
			name:         "responses",
			protocolName: "responses",
			body:         map[string]any{"model": "demo"},
			param:        "input",
		},
		{
			name:         "chat_completions",
			protocolName: "chat_completions",
			body:         map[string]any{"model": "demo", "messages": []any{}},
			param:        "messages",
		},
		{
			name:         "anthropic_messages",
			protocolName: "anthropic_messages",
			body:         map[string]any{"model": "demo", "messages": []any{}},
			param:        "messages",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRequest(tc.protocolName, tc.body)
			requestErr, ok := err.(*RequestError)
			if !ok || requestErr == nil {
				t.Fatalf("expected request error, got %v", err)
			}
			if requestErr.Param != tc.param || requestErr.StatusCode != 400 {
				t.Fatalf("unexpected request error: %#v", requestErr)
			}
		})
	}
}

func TestBuildErrorBodyUsesProtocolShape(t *testing.T) {
	responsesBody := BuildErrorBody("responses", InvalidRequest("bad input", "input"))
	if nestedPathString(responsesBody, "error", "message") != "bad input" || nestedPathString(responsesBody, "error", "param") != "input" {
		t.Fatalf("unexpected responses error body: %#v", responsesBody)
	}

	anthropicBody := BuildErrorBody("anthropic_messages", InvalidRequest("bad input", "messages"))
	if nestedPathString(anthropicBody, "error", "message") != "bad input" {
		t.Fatalf("unexpected anthropic error body: %#v", anthropicBody)
	}
	if got := stringValue(anthropicBody["type"], ""); got != "error" {
		t.Fatalf("unexpected anthropic envelope type: %#v", anthropicBody)
	}
}

func nestedPathString(payload map[string]any, keys ...string) string {
	current := any(payload)
	for _, key := range keys {
		record, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = record[key]
	}
	return stringValue(current, "")
}
