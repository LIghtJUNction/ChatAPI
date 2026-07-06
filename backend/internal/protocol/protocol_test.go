package protocol

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zyf/chatapi/internal/store"
)

type responseFixture struct {
	name       string
	meta       ConversationMeta
	result     TurnResult
	assertBody func(t *testing.T, body map[string]any)
	assertSSE  func(t *testing.T, events []StreamEvent)
}

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

func TestParseRequestSupportsResponsesDirectInputParts(t *testing.T) {
	request := ParseRequest("responses", map[string]any{
		"model": "gpt-test",
		"input": []any{
			map[string]any{"type": "input_text", "text": "hello"},
			map[string]any{"type": "input_image", "image_url": "https://example.com/a.png", "media_type": "image/png"},
		},
	})
	if request.Protocol != ProtocolResponses {
		t.Fatalf("unexpected protocol: %q", request.Protocol)
	}
	if request.UserContent != "hello" {
		t.Fatalf("unexpected user content: %#v", request.UserContent)
	}
	if len(request.InputParts) != 2 {
		t.Fatalf("unexpected direct input parts: %#v", request.InputParts)
	}
	if request.InputParts[0].Type != "text" || request.InputParts[0].Text != "hello" {
		t.Fatalf("unexpected first direct input part: %#v", request.InputParts[0])
	}
	if request.InputParts[1].Type != "image" || request.InputParts[1].URL != "https://example.com/a.png" || request.InputParts[1].MediaType != "image/png" {
		t.Fatalf("unexpected second direct input part: %#v", request.InputParts[1])
	}
}

func TestParseRequestSupportsAnthropicImageSourceBlocks(t *testing.T) {
	request := ParseRequest("anthropic_messages", map[string]any{
		"model": "claude-test",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "describe image"},
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "image/jpeg",
							"data":       "ZmFrZS1iYXNlNjQ=",
						},
					},
				},
			},
		},
	})
	if request.Protocol != ProtocolAnthropicMessages {
		t.Fatalf("unexpected protocol: %q", request.Protocol)
	}
	if request.UserContent != "describe image" {
		t.Fatalf("unexpected user content: %#v", request.UserContent)
	}
	if len(request.InputParts) != 2 {
		t.Fatalf("unexpected anthropic input parts: %#v", request.InputParts)
	}
	if request.InputParts[1].Type != "image" || request.InputParts[1].MediaType != "image/jpeg" || request.InputParts[1].URL != "ZmFrZS1iYXNlNjQ=" {
		t.Fatalf("unexpected anthropic image block: %#v", request.InputParts[1])
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

func TestBuildResponseForMetaMatchesStoredConversationEncoding(t *testing.T) {
	conversation := store.Conversation{
		ResponseID: "resp_meta_match",
		Metadata: map[string]any{
			"request_format": "responses",
			"model":          "chatapi-lab",
		},
	}
	result := TurnResult{
		ResponseID: "resp_meta_match",
		OutputText: "{\"city\":\"tokyo\"}",
		Mode:       "tool_call",
		ToolName:   "lookup_weather",
		ToolCallID: "call_meta_match",
		Usage:      Usage{InputTokens: 1, OutputTokens: 2},
	}
	withConversation := BuildResponse(conversation, result)
	metaOnly := BuildResponseForMeta(ConversationMetaFromConversation(conversation), result)
	delete(withConversation, "conversation")
	if !reflect.DeepEqual(withConversation, metaOnly) {
		t.Fatalf("meta-only response diverged\nwith=%#v\nmeta=%#v", withConversation, metaOnly)
	}
}

func TestBuildStreamCompleteUsesSharedToolPayloads(t *testing.T) {
	result := TurnResult{
		ResponseID: "resp_tool_payload",
		OutputText: "{\"city\":\"tokyo\"}",
		Mode:       "tool_call",
		ToolName:   "lookup_weather",
		ToolCallID: "call_tool_payload",
	}

	responsesEvents := BuildStreamComplete(ConversationMeta{
		Protocol:   ProtocolResponses,
		Model:      "demo",
		ResponseID: result.ResponseID,
	}, result)
	responsePayload := responsesEvents[0].Data.(map[string]any)["response"].(map[string]any)
	output := responsePayload["output"].([]map[string]any)
	if output[0]["call_id"] != "call_tool_payload" || output[0]["name"] != "lookup_weather" {
		t.Fatalf("unexpected responses tool output payload: %#v", output[0])
	}

	chatEvents := BuildStreamComplete(ConversationMeta{
		Protocol: ProtocolChatCompletions,
		Model:    "demo",
	}, result)
	choices := chatEvents[0].Data.(map[string]any)["choices"].([]map[string]any)
	toolCalls := choices[0]["delta"].(map[string]any)["tool_calls"].([]map[string]any)
	if nestedPathString(toolCalls[0], "function", "name") != "lookup_weather" || toolCalls[0]["id"] != "call_tool_payload" {
		t.Fatalf("unexpected chat completions tool call payload: %#v", toolCalls[0])
	}

	anthropicBlock := BuildAnthropicContentBlockStart(result)
	contentBlock := anthropicBlock.Data.(map[string]any)["content_block"].(map[string]any)
	if contentBlock["id"] != "call_tool_payload" || contentBlock["name"] != "lookup_weather" {
		t.Fatalf("unexpected anthropic tool use payload: %#v", contentBlock)
	}
}

func TestProtocolResponseFixtures(t *testing.T) {
	fixtures := []responseFixture{
		{
			name: "responses_assistant_message",
			meta: ConversationMeta{Protocol: ProtocolResponses, Model: "demo", ResponseID: "resp_fixture_1"},
			result: TurnResult{
				ResponseID: "resp_fixture_1",
				OutputText: "hello world",
				Mode:       "assistant_message",
				Usage:      Usage{InputTokens: 3, OutputTokens: 4},
			},
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["object"] != "response" || body["status"] != "completed" || body["output_text"] != "hello world" {
					t.Fatalf("unexpected responses body: %#v", body)
				}
				output := body["output"].([]map[string]any)
				if output[0]["type"] != "message" {
					t.Fatalf("unexpected responses output block: %#v", output[0])
				}
				usage := body["usage"].(map[string]any)
				if usage["total_tokens"] != 7 {
					t.Fatalf("unexpected responses usage: %#v", usage)
				}
			},
			assertSSE: func(t *testing.T, events []StreamEvent) {
				t.Helper()
				if len(events) != 1 || events[0].Event != "response.completed" {
					t.Fatalf("unexpected responses events: %#v", events)
				}
				payload := events[0].Data.(map[string]any)["response"].(map[string]any)
				if payload["output_text"] != "hello world" {
					t.Fatalf("unexpected responses stream payload: %#v", payload)
				}
			},
		},
		{
			name: "chat_completions_tool_call",
			meta: ConversationMeta{Protocol: ProtocolChatCompletions, Model: "demo"},
			result: TurnResult{
				ResponseID: "resp_fixture_2",
				OutputText: "{\"city\":\"tokyo\"}",
				Mode:       "tool_call",
				ToolName:   "lookup_weather",
				ToolCallID: "tool_fixture_1",
				Usage:      Usage{InputTokens: 5, OutputTokens: 6},
			},
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["object"] != "chat.completion" {
					t.Fatalf("unexpected chat completion body: %#v", body)
				}
				choices := body["choices"].([]map[string]any)
				message := choices[0]["message"].(map[string]any)
				toolCalls := message["tool_calls"].([]map[string]any)
				if toolCalls[0]["id"] != "tool_fixture_1" || nestedPathString(toolCalls[0], "function", "name") != "lookup_weather" {
					t.Fatalf("unexpected chat completion tool call body: %#v", toolCalls[0])
				}
			},
			assertSSE: func(t *testing.T, events []StreamEvent) {
				t.Helper()
				if len(events) != 2 || events[1].Data != "[DONE]" {
					t.Fatalf("unexpected chat completion events: %#v", events)
				}
				choices := events[0].Data.(map[string]any)["choices"].([]map[string]any)
				if choices[0]["finish_reason"] != "tool_calls" {
					t.Fatalf("unexpected chat completion finish reason: %#v", choices[0])
				}
			},
		},
		{
			name: "anthropic_tool_call",
			meta: ConversationMeta{Protocol: ProtocolAnthropicMessages, Model: "claude-demo"},
			result: TurnResult{
				ResponseID: "resp_fixture_3",
				OutputText: "{\"city\":\"tokyo\"}",
				Mode:       "tool_call",
				ToolName:   "lookup_weather",
				ToolCallID: "toolu_fixture_1",
				Usage:      Usage{InputTokens: 7, OutputTokens: 8},
			},
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["type"] != "message" || body["stop_reason"] != "end_turn" {
					t.Fatalf("unexpected anthropic body: %#v", body)
				}
				content := body["content"].([]map[string]any)
				if content[0]["id"] != "toolu_fixture_1" || content[0]["name"] != "lookup_weather" {
					t.Fatalf("unexpected anthropic content: %#v", content[0])
				}
			},
			assertSSE: func(t *testing.T, events []StreamEvent) {
				t.Helper()
				if len(events) != 3 || events[0].Event != "content_block_stop" || events[2].Event != "message_stop" {
					t.Fatalf("unexpected anthropic events: %#v", events)
				}
				delta := events[1].Data.(map[string]any)
				if nestedPathString(delta, "delta", "stop_reason") != "tool_use" {
					t.Fatalf("unexpected anthropic stop reason: %#v", delta)
				}
			},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			body := BuildResponseForMeta(fixture.meta, fixture.result)
			fixture.assertBody(t, body)
			events := BuildStreamComplete(fixture.meta, fixture.result)
			fixture.assertSSE(t, events)
		})
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

func TestValidateRequestValidatesToolChoiceAgainstTools(t *testing.T) {
	cases := []struct {
		name    string
		body    map[string]any
		wantErr string
		param   string
	}{
		{
			name: "function choice missing name",
			body: map[string]any{
				"input": "hello",
				"tool_choice": map[string]any{
					"type":     "function",
					"function": map[string]any{},
				},
			},
			wantErr: "is required when type=function",
			param:   "tool_choice.function.name",
		},
		{
			name: "function choice unknown tool",
			body: map[string]any{
				"input": "hello",
				"tools": []any{
					map[string]any{
						"type":     "function",
						"function": map[string]any{"name": "weather"},
					},
				},
				"tool_choice": map[string]any{
					"type":     "function",
					"function": map[string]any{"name": "lookup"},
				},
			},
			wantErr: "must reference a declared tool",
			param:   "tool_choice.function.name",
		},
		{
			name: "invalid string choice",
			body: map[string]any{
				"input":       "hello",
				"tool_choice": "lookup",
			},
			wantErr: "must be one of",
			param:   "tool_choice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRequest("responses", tc.body)
			requestErr, ok := err.(*RequestError)
			if !ok || requestErr == nil {
				t.Fatalf("expected request error, got %v", err)
			}
			if !strings.Contains(requestErr.Message, tc.wantErr) || requestErr.Param != tc.param {
				t.Fatalf("unexpected request error: %#v", requestErr)
			}
		})
	}
}

func TestValidateRequestAcceptsDeclaredToolChoice(t *testing.T) {
	cases := []map[string]any{
		{
			"input": "hello",
			"tool_choice": map[string]any{
				"type":     "function",
				"function": map[string]any{"name": "lookup"},
			},
		},
		{
			"input": "hello",
			"tools": []any{
				map[string]any{
					"type":     "function",
					"function": map[string]any{"name": "lookup"},
				},
			},
			"tool_choice": map[string]any{
				"type":     "function",
				"function": map[string]any{"name": "lookup"},
			},
		},
	}
	for i, body := range cases {
		err := ValidateRequest("responses", body)
		if err != nil {
			t.Fatalf("case %d expected valid request, got %v", i, err)
		}
	}
}

func TestValidateRequestValidatesResponseFormat(t *testing.T) {
	cases := []struct {
		name    string
		body    map[string]any
		param   string
		wantErr string
	}{
		{
			name: "missing json_schema object",
			body: map[string]any{
				"input": "hello",
				"response_format": map[string]any{
					"type": "json_schema",
				},
			},
			param:   "response_format.json_schema",
			wantErr: "is required when type=json_schema",
		},
		{
			name: "missing schema name",
			body: map[string]any{
				"input": "hello",
				"response_format": map[string]any{
					"type":        "json_schema",
					"json_schema": map[string]any{"schema": map[string]any{"type": "object"}},
				},
			},
			param:   "response_format.json_schema.name",
			wantErr: "name is required",
		},
		{
			name: "missing schema body",
			body: map[string]any{
				"input": "hello",
				"response_format": map[string]any{
					"type":        "json_schema",
					"json_schema": map[string]any{"name": "answer"},
				},
			},
			param:   "response_format.json_schema.schema",
			wantErr: "schema is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRequest("responses", tc.body)
			requestErr, ok := err.(*RequestError)
			if !ok || requestErr == nil {
				t.Fatalf("expected request error, got %v", err)
			}
			if !strings.Contains(requestErr.Message, tc.wantErr) || requestErr.Param != tc.param {
				t.Fatalf("unexpected request error: %#v", requestErr)
			}
		})
	}
}

func TestValidateRequestAcceptsJSONSchemaResponseFormat(t *testing.T) {
	err := ValidateRequest("responses", map[string]any{
		"input": "hello",
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "answer",
				"schema": map[string]any{
					"type": "object",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected valid response format, got %v", err)
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
