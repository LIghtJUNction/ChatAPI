package protocol

import "github.com/google/uuid"

type StreamEvent struct {
	Event string
	Data  any
	Done  bool
}

func BuildStreamStart(meta ConversationMeta) []StreamEvent {
	responseID := stringValue(meta.ResponseID, "resp_"+uuid.NewString())
	switch meta.Protocol {
	case ProtocolChatCompletions:
		return []StreamEvent{{
			Data: map[string]any{
				"id":     "chatcmpl_" + uuid.NewString(),
				"object": "chat.completion.chunk",
				"model":  meta.Model,
				"choices": []map[string]any{{
					"index": 0,
					"delta": map[string]any{"role": "assistant"},
				}},
			},
		}}
	case ProtocolAnthropicMessages:
		return []StreamEvent{{
			Event: "message_start",
			Data: map[string]any{
				"type": "message_start",
				"message": map[string]any{
					"id":      "msg_" + uuid.NewString(),
					"type":    "message",
					"role":    "assistant",
					"model":   meta.Model,
					"content": []any{},
				},
			},
		}}
	default:
		return []StreamEvent{{
			Event: "response.created",
			Data: map[string]any{
				"type": "response.created",
				"response": map[string]any{
					"id":     responseID,
					"object": "response",
					"status": "in_progress",
					"model":  meta.Model,
				},
			},
		}}
	}
}

func BuildStreamDelta(meta ConversationMeta, deltaText string) []StreamEvent {
	switch meta.Protocol {
	case ProtocolChatCompletions:
		return []StreamEvent{{
			Data: map[string]any{
				"id":     "chatcmpl_" + uuid.NewString(),
				"object": "chat.completion.chunk",
				"model":  meta.Model,
				"choices": []map[string]any{{
					"index": 0,
					"delta": map[string]any{"content": deltaText},
				}},
			},
		}}
	case ProtocolAnthropicMessages:
		return []StreamEvent{{
			Event: "content_block_delta",
			Data: map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{
					"type": "text_delta",
					"text": deltaText,
				},
			},
		}}
	default:
		return []StreamEvent{{
			Event: "response.output_text.delta",
			Data: map[string]any{
				"type":  "response.output_text.delta",
				"delta": deltaText,
			},
		}}
	}
}

func BuildStreamComplete(meta ConversationMeta, result TurnResult) []StreamEvent {
	usage := normalizeUsage(result.Usage)
	switch meta.Protocol {
	case ProtocolChatCompletions:
		chunk := map[string]any{
			"id":     chatCompletionID(result),
			"object": "chat.completion.chunk",
			"model":  meta.Model,
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "stop",
			}},
		}
		if result.Mode == "tool_call" {
			chunk["choices"] = []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []map[string]any{buildChatCompletionToolCall(result)},
				},
				"finish_reason": "tool_calls",
			}}
		}
		return []StreamEvent{{Data: chunk}, {Data: "[DONE]", Done: true}}
	case ProtocolAnthropicMessages:
		stopReason := "end_turn"
		if result.Mode == "tool_call" {
			stopReason = "tool_use"
		}
		return []StreamEvent{
			{
				Event: "content_block_stop",
				Data: map[string]any{
					"type":  "content_block_stop",
					"index": 0,
				},
			},
			{
				Event: "message_delta",
				Data: map[string]any{
					"type": "message_delta",
					"delta": map[string]any{
						"stop_reason": stopReason,
					},
					"usage": map[string]any{
						"input_tokens":  usage.InputTokens,
						"output_tokens": usage.OutputTokens,
					},
				},
			},
			{
				Event: "message_stop",
				Data: map[string]any{
					"type": "message_stop",
				},
			},
		}
	default:
		return []StreamEvent{{
			Event: "response.completed",
			Data: map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"id":          result.ResponseID,
					"object":      "response",
					"status":      "completed",
					"output_text": result.OutputText,
					"usage": map[string]any{
						"input_tokens":  usage.InputTokens,
						"output_tokens": usage.OutputTokens,
						"total_tokens":  usage.TotalTokens,
					},
					"output": buildResponsesOutput(result),
				},
			},
		}}
	}
}

func BuildAnthropicContentBlockStart(result TurnResult) StreamEvent {
	block := map[string]any{
		"type": "text",
		"text": "",
	}
	if result.Mode == "tool_call" {
		block = buildAnthropicToolUseBlock(result)
	}
	return StreamEvent{
		Event: "content_block_start",
		Data: map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": block,
		},
	}
}

func BuildStreamAbort(meta ConversationMeta, body map[string]any) []StreamEvent {
	switch meta.Protocol {
	case ProtocolChatCompletions:
		return []StreamEvent{{Data: body}, {Data: "[DONE]", Done: true}}
	case ProtocolAnthropicMessages:
		return []StreamEvent{{
			Event: "error",
			Data:  body,
		}}
	default:
		return []StreamEvent{{
			Event: "response.error",
			Data:  body,
		}}
	}
}
