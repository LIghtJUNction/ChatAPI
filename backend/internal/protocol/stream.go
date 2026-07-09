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

func BuildResponsesReasoningDelta(result TurnResult, deltaText string) []StreamEvent {
	itemID := "rs_" + uuid.NewString()
	itemIndex := 0
	item := map[string]any{
		"id":     itemID,
		"type":   "reasoning",
		"status": "completed",
		"summary": []map[string]any{{
			"type": "summary_text",
			"text": deltaText,
		}},
		"content": []map[string]any{{
			"type": "reasoning_text",
			"text": deltaText,
		}},
	}
	events := []StreamEvent{{
		Event: "response.output_item.added",
		Data: map[string]any{
			"type": "response.output_item.added",
			"item": map[string]any{
				"id":      itemID,
				"type":    "reasoning",
				"status":  "in_progress",
				"summary": []any{},
				"content": []any{},
			},
			"output_index": itemIndex,
		},
	}}
	if result.ReasoningStreamMode == "reasoning" || result.ReasoningStreamMode == "reasoning_text" {
		events = append(events,
			StreamEvent{
				Event: "response.content_part.added",
				Data: map[string]any{
					"type":          "response.content_part.added",
					"content_index": 0,
					"item_id":       itemID,
					"output_index":  itemIndex,
					"part": map[string]any{
						"type": "reasoning_text",
						"text": "",
					},
				},
			},
			StreamEvent{
				Event: "response.reasoning_text.delta",
				Data: map[string]any{
					"type":          "response.reasoning_text.delta",
					"content_index": 0,
					"delta":         deltaText,
					"item_id":       itemID,
					"output_index":  itemIndex,
				},
			},
			StreamEvent{
				Event: "response.reasoning_text.done",
				Data: map[string]any{
					"type":          "response.reasoning_text.done",
					"content_index": 0,
					"item_id":       itemID,
					"output_index":  itemIndex,
					"text":          deltaText,
				},
			},
			StreamEvent{
				Event: "response.content_part.done",
				Data: map[string]any{
					"type":          "response.content_part.done",
					"content_index": 0,
					"item_id":       itemID,
					"output_index":  itemIndex,
					"part": map[string]any{
						"type": "reasoning_text",
						"text": deltaText,
					},
				},
			},
		)
	} else {
		events = append(events,
			StreamEvent{
				Event: "response.reasoning_summary_part.added",
				Data: map[string]any{
					"type":          "response.reasoning_summary_part.added",
					"item_id":       itemID,
					"output_index":  itemIndex,
					"summary_index": 0,
					"part": map[string]any{
						"type": "summary_text",
						"text": "",
					},
				},
			},
			StreamEvent{
				Event: "response.reasoning_summary_text.delta",
				Data: map[string]any{
					"type":          "response.reasoning_summary_text.delta",
					"delta":         deltaText,
					"item_id":       itemID,
					"output_index":  itemIndex,
					"summary_index": 0,
				},
			},
			StreamEvent{
				Event: "response.reasoning_summary_text.done",
				Data: map[string]any{
					"type":          "response.reasoning_summary_text.done",
					"item_id":       itemID,
					"output_index":  itemIndex,
					"summary_index": 0,
					"text":          deltaText,
				},
			},
			StreamEvent{
				Event: "response.reasoning_summary_part.done",
				Data: map[string]any{
					"type":          "response.reasoning_summary_part.done",
					"item_id":       itemID,
					"output_index":  itemIndex,
					"summary_index": 0,
					"part": map[string]any{
						"type": "summary_text",
						"text": deltaText,
					},
				},
			},
		)
	}
	events = append(events, StreamEvent{
		Event: "response.output_item.done",
		Data: map[string]any{
			"type":         "response.output_item.done",
			"output_index": itemIndex,
			"item":         item,
		},
	})
	return events
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
		return nil
	case ProtocolAnthropicMessages:
		return []StreamEvent{{
			Event: "error",
			Data:  body,
		}}
	default:
		errorPayload, _ := body["error"].(map[string]any)
		return []StreamEvent{{
			Event: "response.failed",
			Data: map[string]any{
				"type": "response.failed",
				"response": map[string]any{
					"id":          stringValue(meta.ResponseID, "resp_"+uuid.NewString()),
					"object":      "response",
					"status":      "failed",
					"model":       meta.Model,
					"output":      []any{},
					"output_text": "",
					"error":       errorPayload,
				},
			},
		}}
	}
}
