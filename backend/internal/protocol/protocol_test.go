package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
)

type responseFixture struct {
	name       string
	meta       ConversationMeta
	result     TurnResult
	assertBody func(t *testing.T, body map[string]any)
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
	if request.ToolSchemas[0].Name != "lookup" || request.ToolSchemas[0].Type != "function" || request.ToolSchemas[0].Raw["name"] != "lookup" {
		t.Fatalf("unexpected normalized tool schema: %#v", request.ToolSchemas[0])
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

func TestParseRequestSupportsResponsesMessageContentWithoutRole(t *testing.T) {
	request := ParseRequest("responses", map[string]any{
		"model": "gpt-test",
		"input": []any{
			map[string]any{
				"content": []any{
					map[string]any{"type": "input_text", "text": "hello"},
					map[string]any{"type": "input_image", "image_url": "data:image/png;base64,ZmFrZQ==", "media_type": "image/png"},
				},
			},
		},
	})
	if request.UserContent != "hello" {
		t.Fatalf("unexpected user content: %#v", request.UserContent)
	}
	if len(request.InputParts) != 2 {
		t.Fatalf("unexpected responses message content parts: %#v", request.InputParts)
	}
	if request.InputParts[0].Type != "text" || request.InputParts[1].Type != "image" {
		t.Fatalf("unexpected responses parsed parts: %#v", request.InputParts)
	}
	if request.InputParts[1].URL != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("unexpected responses image url: %#v", request.InputParts[1])
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

func TestParseRequestSupportsResponsesFunctionCallOutputInput(t *testing.T) {
	request := ParseRequest("responses", map[string]any{
		"model": "gpt-test",
		"input": []any{
			map[string]any{"type": "function_call_output", "output": "{\"ok\":true}"},
		},
	})
	if request.UserContent != "{\"ok\":true}" {
		t.Fatalf("unexpected user content: %#v", request.UserContent)
	}
	if len(request.InputParts) != 1 || request.InputParts[0].Type != "tool_result" || request.InputParts[0].Text != "{\"ok\":true}" {
		t.Fatalf("unexpected function_call_output parsing: %#v", request.InputParts)
	}
}

func TestParseRequestSupportsChatCompletionsToolMessage(t *testing.T) {
	request := ParseRequest("chat_completions", map[string]any{
		"model": "gpt-test",
		"messages": []any{
			map[string]any{"role": "assistant", "content": "calling tool"},
			map[string]any{"role": "tool", "content": "{\"ok\":true}"},
		},
	})
	if request.UserContent != "{\"ok\":true}" {
		t.Fatalf("unexpected tool message user content: %#v", request.UserContent)
	}
	if len(request.InputParts) != 1 || request.InputParts[0].Type != "tool_result" {
		t.Fatalf("unexpected tool message input parts: %#v", request.InputParts)
	}
}

func TestParseRequestSupportsAnthropicToolResultBlocks(t *testing.T) {
	request := ParseRequest("anthropic_messages", map[string]any{
		"model": "claude-test",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "toolu_1",
						"content": []any{
							map[string]any{"type": "text", "text": "weather is sunny"},
							map[string]any{"type": "text", "text": "temperature 25C"},
						},
					},
				},
			},
		},
	})
	if request.UserContent != "weather is sunny\ntemperature 25C" {
		t.Fatalf("unexpected anthropic tool result content: %#v", request.UserContent)
	}
	if len(request.InputParts) != 1 || request.InputParts[0].Type != "tool_result" {
		t.Fatalf("unexpected anthropic tool result parts: %#v", request.InputParts)
	}
}

func TestParseRequestCapturesSystemAndDeveloperContent(t *testing.T) {
	request := ParseRequest("chat_completions", map[string]any{
		"model": "gpt-test",
		"messages": []any{
			map[string]any{"role": "system", "content": "system policy"},
			map[string]any{"role": "developer", "content": []any{
				map[string]any{"type": "text", "text": "developer note"},
			}},
			map[string]any{"role": "assistant", "content": "assistant context"},
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if request.SystemContent != "system policy" {
		t.Fatalf("unexpected system content: %#v", request.SystemContent)
	}
	if request.DeveloperContent != "developer note" {
		t.Fatalf("unexpected developer content: %#v", request.DeveloperContent)
	}
	if request.AssistantContent != "assistant context" {
		t.Fatalf("unexpected assistant content: %#v", request.AssistantContent)
	}
	if request.UserContent != "hello" {
		t.Fatalf("unexpected user content: %#v", request.UserContent)
	}
}

func TestParseRequestCapturesAnthropicTopLevelSystem(t *testing.T) {
	request := ParseRequest("anthropic_messages", map[string]any{
		"model":  "claude-test",
		"system": []any{map[string]any{"type": "text", "text": "follow policy"}},
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if request.SystemContent != "follow policy" {
		t.Fatalf("unexpected top-level anthropic system content: %#v", request.SystemContent)
	}
	if request.UserContent != "hello" {
		t.Fatalf("unexpected user content: %#v", request.UserContent)
	}
}

func TestParseRequestCapturesResponsesAssistantInputMessages(t *testing.T) {
	request := ParseRequest("responses", map[string]any{
		"model": "gpt-test",
		"input": []any{
			map[string]any{
				"type":    "message",
				"role":    "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": "previous answer"}},
			},
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": []any{map[string]any{"type": "input_text", "text": "next question"}},
			},
		},
	})
	if request.AssistantContent != "previous answer" {
		t.Fatalf("unexpected responses assistant content: %#v", request.AssistantContent)
	}
	if request.UserContent != "next question" {
		t.Fatalf("unexpected responses user content: %#v", request.UserContent)
	}
}

func TestNormalizeToolSchemasSupportsOpenAIAndAnthropicShapes(t *testing.T) {
	normalized := NormalizeToolSchemas([]any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "lookup_weather",
				"description": "lookup weather",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
		map[string]any{
			"name":        "write_note",
			"description": "write note",
			"input_schema": map[string]any{
				"type": "object",
			},
		},
		map[string]any{
			"type": "custom",
			"name": "custom_tool",
		},
		"invalid",
		map[string]any{"function": map[string]any{}},
	})
	if len(normalized) != 3 {
		t.Fatalf("unexpected normalized tool schema count: %#v", normalized)
	}
	if normalized[0].Name != "lookup_weather" || normalized[0].Type != "function" || normalized[0].Parameters["type"] != "object" {
		t.Fatalf("unexpected openai tool normalization: %#v", normalized[0])
	}
	if normalized[1].Name != "write_note" || normalized[1].Type != "function" || normalized[1].Parameters["type"] != "object" {
		t.Fatalf("unexpected anthropic tool normalization: %#v", normalized[1])
	}
	if normalized[2].Name != "custom_tool" || normalized[2].Type != "custom" {
		t.Fatalf("unexpected passthrough tool normalization: %#v", normalized[2])
	}
}

func TestRawToolSchemasRetainsOriginalDefinitions(t *testing.T) {
	tools := normalizeToolSchemasDetailed([]any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "lookup_weather",
				"description": "lookup weather",
				"parameters": map[string]any{
					"type": "object",
				},
			},
			"x-provider-hint": "openai",
		},
		map[string]any{
			"type":        "custom",
			"name":        "write_note",
			"description": "write note",
			"input_schema": map[string]any{
				"type": "object",
			},
			"x-provider-hint": "anthropic",
		},
	})
	raw := RawToolSchemas(tools)
	if len(raw) != 2 {
		t.Fatalf("unexpected raw tool schema count: %#v", raw)
	}
	first := raw[0].(map[string]any)
	second := raw[1].(map[string]any)
	if first["x-provider-hint"] != "openai" {
		t.Fatalf("unexpected first raw tool schema: %#v", first)
	}
	if second["x-provider-hint"] != "anthropic" {
		t.Fatalf("unexpected second raw tool schema: %#v", second)
	}
	if tools[0].Parameters["type"] != "object" || tools[1].Parameters["type"] != "object" {
		t.Fatalf("unexpected normalized parameters: %#v", tools)
	}
}

func TestParseRequestSeparatesResponsesBuiltinTools(t *testing.T) {
	request := ParseRequest("responses", map[string]any{
		"model": "gpt-test",
		"input": "hello",
		"tools": []any{
			map[string]any{"type": "web_search"},
			map[string]any{"type": "image_generation", "model": "gpt-image-2"},
			map[string]any{
				"type": "function",
				"name": "lookup_weather",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	})
	if len(request.BuiltinTools) != 2 {
		t.Fatalf("unexpected builtin tools: %#v", request.BuiltinTools)
	}
	if request.BuiltinTools[0].Kind != "web_search" || request.BuiltinTools[1].Kind != "image_generation" {
		t.Fatalf("unexpected builtin tool kinds: %#v", request.BuiltinTools)
	}
	if len(request.ToolSchemas) != 1 || request.ToolSchemas[0].Name != "lookup_weather" {
		t.Fatalf("builtin tools leaked into function schemas: %#v", request.ToolSchemas)
	}
	body := BuildRequestBody(request)
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 3 {
		t.Fatalf("rebuilt responses tools lost builtin tools: %#v", body["tools"])
	}
}

func TestParseRequestLastUserContentIsProtocolIndependent(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     map[string]any
	}{
		{
			name: "chat tool follow-up", protocol: "chat_completions",
			body: map[string]any{"messages": []any{
				map[string]any{"role": "user", "content": "human question"},
				map[string]any{"role": "assistant", "content": ""},
				map[string]any{"role": "tool", "tool_call_id": "call", "content": "tool output"},
			}},
		},
		{
			name: "anthropic tool result", protocol: "anthropic_messages",
			body: map[string]any{"messages": []any{
				map[string]any{"role": "user", "content": "human question"},
				map[string]any{"role": "assistant", "content": ""},
				map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": "tool output"}}},
			}},
		},
		{
			name: "responses history", protocol: "responses",
			body: map[string]any{"input": []any{
				map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "old"}}},
				map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "human question"}}},
			}},
		},
		{
			name: "responses direct input text", protocol: "responses",
			body: map[string]any{"input": []any{map[string]any{"type": "input_text", "text": "human question"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := ParseRequest(tc.protocol, tc.body)
			if request.LastUserContent != "human question" {
				t.Fatalf("unexpected last user content: %q", request.LastUserContent)
			}
		})
	}
}

func TestBuildResponseToolResultResponses(t *testing.T) {
	body := BuildResponseForMeta(ConversationMeta{
		Protocol:   ProtocolResponses,
		Model:      "chatapi-lab",
		ResponseID: "resp_2",
	}, TurnResult{
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
			body := BuildResponseForMeta(ConversationMeta{
				Protocol:   ParseProtocol(tc.requestFormat),
				Model:      "test-model",
				ResponseID: tc.result.ResponseID,
			}, tc.result)
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

func TestBuildResponseForMetaMatchesStoredConversationEncoding(t *testing.T) {
	meta := ConversationMeta{Protocol: ProtocolResponses, Model: "chatapi-lab", ResponseID: "resp_meta_match"}
	result := TurnResult{
		ResponseID: "resp_meta_match",
		OutputText: "{\"city\":\"tokyo\"}",
		Mode:       "tool_call",
		ToolName:   "lookup_weather",
		ToolCallID: "call_meta_match",
		Usage:      Usage{InputTokens: 1, OutputTokens: 2},
	}
	metaOnly := BuildResponseForMeta(meta, result)
	second := BuildResponseForMeta(meta, result)
	if !reflect.DeepEqual(metaOnly, second) {
		t.Fatalf("meta-only response diverged\nfirst=%#v\nsecond=%#v", metaOnly, second)
	}
}

func TestNormalizeRequestSupportsOpenAIResponsesSDKJSON(t *testing.T) {
	bodyBytes, err := json.Marshal(responses.ResponseNewParams{
		Model: "gpt-4.1-mini",
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam{
				responses.ResponseInputItemParamOfMessage(
					responses.ResponseInputMessageContentListParam{
						responses.ResponseInputContentParamOfInputText("look at this"),
						{
							OfInputImage: &responses.ResponseInputImageParam{
								ImageURL: openai.String("https://example.com/demo.png"),
								Detail:   responses.ResponseInputImageDetailHigh,
							},
						},
					},
					responses.EasyInputMessageRoleUser,
				),
			},
		},
		Tools: []responses.ToolUnionParam{{
			OfFunction: &responses.FunctionToolParam{
				Name:        "lookup_weather",
				Description: openai.String("lookup weather"),
				Parameters:  map[string]any{"type": "object"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal responses params: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("unmarshal responses body: %v", err)
	}
	request, err := NormalizeRequest("responses", body)
	if err != nil {
		t.Fatalf("normalize responses sdk body: %v", err)
	}
	if request.Protocol != ProtocolResponses || request.UserContent != "look at this" {
		t.Fatalf("unexpected normalized responses request: %#v", request)
	}
	if len(request.InputParts) != 2 || request.InputParts[1].Type != "image" || request.InputParts[1].URL != "https://example.com/demo.png" {
		t.Fatalf("unexpected normalized responses input parts: %#v", request.InputParts)
	}
	if len(request.ToolSchemas) != 1 || request.ToolSchemas[0].Name != "lookup_weather" {
		t.Fatalf("unexpected normalized responses tools: %#v", request.ToolSchemas)
	}
}

func TestNormalizeRequestSupportsOpenAIChatSDKJSON(t *testing.T) {
	bodyBytes, err := json.Marshal(openai.ChatCompletionNewParams{
		Model: "gpt-4.1-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
				openai.TextContentPart("describe image"),
				openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: "https://example.com/cat.png",
				}),
			}),
		},
		Tools: []openai.ChatCompletionToolParam{{
			Function: openai.FunctionDefinitionParam{
				Name:        "describe_image",
				Description: openai.String("describe image"),
				Parameters:  map[string]any{"type": "object"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal chat completion params: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("unmarshal chat completion body: %v", err)
	}
	request, err := NormalizeRequest("chat_completions", body)
	if err != nil {
		t.Fatalf("normalize chat sdk body: %v", err)
	}
	if request.Protocol != ProtocolChatCompletions || request.UserContent != "describe image" {
		t.Fatalf("unexpected normalized chat request: %#v", request)
	}
	if len(request.InputParts) != 2 || request.InputParts[1].Type != "image" || request.InputParts[1].URL != "https://example.com/cat.png" {
		t.Fatalf("unexpected normalized chat input parts: %#v", request.InputParts)
	}
	if len(request.ToolSchemas) != 1 || request.ToolSchemas[0].Name != "describe_image" {
		t.Fatalf("unexpected normalized chat tools: %#v", request.ToolSchemas)
	}
}

func TestNormalizeRequestSupportsAnthropicSDKJSON(t *testing.T) {
	bodyBytes, err := json.Marshal(anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 128,
		System:    []anthropic.TextBlockParam{{Text: "follow policy"}},
		Messages: []anthropic.MessageParam{{
			Role: anthropic.MessageParamRoleUser,
			Content: []anthropic.ContentBlockParamUnion{
				{OfText: &anthropic.TextBlockParam{Text: "what is in image?"}},
				{OfImage: &anthropic.ImageBlockParam{
					Source: anthropic.ImageBlockParamSourceUnion{
						OfURL: &anthropic.URLImageSourceParam{
							URL: "https://example.com/tree.png",
						},
					},
				}},
			},
		}},
		Tools: []anthropic.ToolUnionParam{{
			OfTool: &anthropic.ToolParam{
				Name:        "inspect_image",
				Description: anthropic.String("inspect image"),
				InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{"type": "object"}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal anthropic params: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("unmarshal anthropic body: %v", err)
	}
	request, err := NormalizeRequest("anthropic_messages", body)
	if err != nil {
		t.Fatalf("normalize anthropic sdk body: %v", err)
	}
	if request.Protocol != ProtocolAnthropicMessages || request.SystemContent != "follow policy" || request.UserContent != "what is in image?" {
		t.Fatalf("unexpected normalized anthropic request: %#v", request)
	}
	if len(request.InputParts) != 2 || request.InputParts[1].Type != "image" || request.InputParts[1].URL != "https://example.com/tree.png" {
		t.Fatalf("unexpected normalized anthropic input parts: %#v", request.InputParts)
	}
	if len(request.ToolSchemas) != 1 || request.ToolSchemas[0].Name != "inspect_image" {
		t.Fatalf("unexpected normalized anthropic tools: %#v", request.ToolSchemas)
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
				if body["type"] != "message" || body["stop_reason"] != "tool_use" {
					t.Fatalf("unexpected anthropic body: %#v", body)
				}
				content := body["content"].([]map[string]any)
				if content[0]["id"] != "toolu_fixture_1" || content[0]["name"] != "lookup_weather" {
					t.Fatalf("unexpected anthropic content: %#v", content[0])
				}
			},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			body := BuildResponseForMeta(fixture.meta, fixture.result)
			fixture.assertBody(t, body)
		})
	}
}

func TestBuildResponseForMetaMapsOutputGuardFinishReasons(t *testing.T) {
	responses := BuildResponseForMeta(ConversationMeta{Protocol: ProtocolResponses, Model: "gpt-test"}, TurnResult{
		ResponseID: "resp_test", OutputText: "partial", FinishReason: "length", Usage: Usage{OutputTokens: 4},
	})
	if responses["status"] != "incomplete" || responses["incomplete_details"].(map[string]any)["reason"] != "max_output_tokens" {
		t.Fatalf("unexpected responses incomplete body: %#v", responses)
	}

	chat := BuildResponseForMeta(ConversationMeta{Protocol: ProtocolChatCompletions, Model: "gpt-test"}, TurnResult{
		ResponseID: "chatcmpl_test", OutputText: "partial", FinishReason: "length", Usage: Usage{OutputTokens: 4},
	})
	chatChoices := chat["choices"].([]map[string]any)
	if chatChoices[0]["finish_reason"] != "length" {
		t.Fatalf("unexpected chat finish body: %#v", chat)
	}

	anthropic := BuildResponseForMeta(ConversationMeta{Protocol: ProtocolAnthropicMessages, Model: "claude-test"}, TurnResult{
		ResponseID: "msg_test", OutputText: "partial", FinishReason: "stop_sequence", StopSequence: "END", Usage: Usage{OutputTokens: 4},
	})
	if anthropic["stop_reason"] != "stop_sequence" || anthropic["stop_sequence"] != "END" {
		t.Fatalf("unexpected anthropic stop body: %#v", anthropic)
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

func TestParseRequestCapturesResponsesOptionsAndRawBody(t *testing.T) {
	store := false
	_ = store
	request := ParseRequest("responses", map[string]any{
		"model":                "gpt-responses",
		"input":                "hello",
		"stream":               true,
		"instructions":         "be terse",
		"previous_response_id": "resp_prev",
		"store":                false,
		"metadata":             map[string]any{"case": "responses"},
		"include":              []any{"reasoning.encrypted_content"},
		"max_output_tokens":    float64(128),
		"parallel_tool_calls":  true,
		"reasoning":            map[string]any{"effort": "low"},
		"service_tier":         "flex",
		"stream_options":       map[string]any{"include_usage": true},
		"temperature":          0.4,
		"top_p":                0.9,
		"text":                 map[string]any{"format": map[string]any{"type": "json_schema", "name": "answer", "schema": map[string]any{"type": "object"}}},
		"truncation":           "auto",
		"user":                 "end-user-1",
		"unknown_vendor_field": map[string]any{"kept": true},
	})
	if request.Options.Instructions != "be terse" || request.Options.PreviousResponseID != "resp_prev" {
		t.Fatalf("missing responses options: %#v", request.Options)
	}
	if request.Options.Store == nil || *request.Options.Store {
		t.Fatalf("unexpected store option: %#v", request.Options.Store)
	}
	if request.Options.MaxOutputTokens == nil || *request.Options.MaxOutputTokens != 128 {
		t.Fatalf("unexpected max output option: %#v", request.Options.MaxOutputTokens)
	}
	if request.ResponseFormat.Type != "json_schema" || request.ResponseFormat.Name != "answer" {
		t.Fatalf("unexpected responses text format: %#v", request.ResponseFormat)
	}
	if request.RawBody["unknown_vendor_field"] == nil || request.Options.ProviderExtras["unknown_vendor_field"] == nil {
		t.Fatalf("expected raw body and provider extras to retain unknown field: raw=%#v extras=%#v", request.RawBody, request.Options.ProviderExtras)
	}

	body := BuildRequestBody(request)
	if body["response_format"] != nil {
		t.Fatalf("responses rebuild should not use response_format: %#v", body)
	}
	if nestedPathString(body, "text", "format", "name") != "answer" {
		t.Fatalf("responses rebuild lost text.format: %#v", body["text"])
	}
	if body["unknown_vendor_field"] == nil {
		t.Fatalf("responses rebuild lost provider extra: %#v", body)
	}
}

func TestParseRequestCapturesChatCompletionsOptions(t *testing.T) {
	request := ParseRequest("chat_completions", map[string]any{
		"model":                 "gpt-chat",
		"messages":              []any{map[string]any{"role": "user", "content": "hello"}},
		"stream":                true,
		"max_tokens":            float64(40),
		"max_completion_tokens": float64(30),
		"temperature":           0.2,
		"top_p":                 0.8,
		"stop":                  []any{"END"},
		"n":                     float64(1),
		"presence_penalty":      0.1,
		"frequency_penalty":     0.2,
		"seed":                  float64(123),
		"user":                  "end-user-2",
		"stream_options":        map[string]any{"include_usage": true},
		"parallel_tool_calls":   false,
		"reasoning_effort":      "medium",
		"modalities":            []any{"text"},
		"audio":                 map[string]any{"voice": "alloy"},
		"prediction":            map[string]any{"type": "content", "content": "prefill"},
		"metadata":              map[string]any{"case": "chat"},
		"service_tier":          "default",
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "chat_answer",
				"schema": map[string]any{"type": "object"},
			},
		},
		"vendor_only": "kept",
	})
	if request.Options.MaxTokens == nil || *request.Options.MaxTokens != 40 {
		t.Fatalf("unexpected max_tokens: %#v", request.Options.MaxTokens)
	}
	if request.Options.MaxCompletionTokens == nil || *request.Options.MaxCompletionTokens != 30 {
		t.Fatalf("unexpected max_completion_tokens: %#v", request.Options.MaxCompletionTokens)
	}
	if len(request.Options.Stop) != 1 || request.Options.Stop[0] != "END" {
		t.Fatalf("unexpected stop: %#v", request.Options.Stop)
	}
	if request.Options.Seed == nil || *request.Options.Seed != 123 {
		t.Fatalf("unexpected seed: %#v", request.Options.Seed)
	}
	if request.ResponseFormat.Name != "chat_answer" {
		t.Fatalf("unexpected response format: %#v", request.ResponseFormat)
	}

	body := BuildRequestBody(request)
	if nestedPathString(body, "response_format", "json_schema", "name") != "chat_answer" {
		t.Fatalf("chat rebuild lost response_format: %#v", body["response_format"])
	}
	if body["vendor_only"] != "kept" {
		t.Fatalf("chat rebuild lost provider extra: %#v", body)
	}
}

func TestParseRequestCapturesAnthropicOptions(t *testing.T) {
	request := ParseRequest("anthropic_messages", map[string]any{
		"model":              "claude-test",
		"messages":           []any{map[string]any{"role": "user", "content": "hello"}},
		"stream":             true,
		"max_tokens":         float64(64),
		"temperature":        0.3,
		"top_p":              0.7,
		"top_k":              float64(40),
		"stop_sequences":     []any{"STOP"},
		"metadata":           map[string]any{"user_id": "u1"},
		"thinking":           map[string]any{"type": "enabled", "budget_tokens": 1024},
		"service_tier":       "standard_only",
		"mcp_servers":        []any{map[string]any{"type": "url", "url": "https://mcp.example"}},
		"context_management": map[string]any{"edits": []any{}},
		"anthropic_extra":    "kept",
	})
	if request.Options.MaxTokens == nil || *request.Options.MaxTokens != 64 {
		t.Fatalf("unexpected max tokens: %#v", request.Options.MaxTokens)
	}
	if request.Options.TopK == nil || *request.Options.TopK != 40 {
		t.Fatalf("unexpected top_k: %#v", request.Options.TopK)
	}
	if len(request.Options.Stop) != 1 || request.Options.Stop[0] != "STOP" {
		t.Fatalf("unexpected stop sequences: %#v", request.Options.Stop)
	}
	if len(request.Options.Thinking) == 0 || len(request.Options.MCPServers) != 1 || len(request.Options.ContextManagement) == 0 {
		t.Fatalf("missing anthropic provider options: %#v", request.Options)
	}

	body := BuildRequestBody(request)
	if body["anthropic_extra"] != "kept" {
		t.Fatalf("anthropic rebuild lost provider extra: %#v", body)
	}
	if body["stop_sequences"] == nil || body["thinking"] == nil || body["mcp_servers"] == nil {
		t.Fatalf("anthropic rebuild lost options: %#v", body)
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
