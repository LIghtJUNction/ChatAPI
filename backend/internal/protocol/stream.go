package protocol

import (
	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/store"
)

type StreamEvent struct {
	Event string
	Data  any
	Done  bool
}

func BuildStreamStart(conversation store.Conversation) []StreamEvent {
	format := stringValue(conversation.Metadata["request_format"], "responses")
	responseID := stringValue(conversation.ResponseID, "resp_"+uuid.NewString())
	model := stringValue(conversation.Metadata["model"], "chatapi-lab")

	switch format {
	case "chat_completions":
		return []StreamEvent{{
			Data: map[string]any{
				"id":     "chatcmpl_" + uuid.NewString(),
				"object": "chat.completion.chunk",
				"model":  model,
				"choices": []map[string]any{{
					"index": 0,
					"delta": map[string]any{"role": "assistant"},
				}},
			},
		}}
	case "anthropic_messages":
		return []StreamEvent{
			{
				Event: "message_start",
				Data: map[string]any{
					"type": "message_start",
					"message": map[string]any{
						"id":      "msg_" + uuid.NewString(),
						"type":    "message",
						"role":    "assistant",
						"model":   model,
						"content": []any{},
					},
				},
			},
			{
				Event: "content_block_start",
				Data: map[string]any{
					"type":  "content_block_start",
					"index": 0,
					"content_block": map[string]any{
						"type": "text",
						"text": "",
					},
				},
			},
		}
	default:
		return []StreamEvent{{
			Event: "response.created",
			Data: map[string]any{
				"type": "response.created",
				"response": map[string]any{
					"id":     responseID,
					"object": "response",
					"status": "in_progress",
					"model":  model,
				},
			},
		}}
	}
}

func BuildStreamDelta(conversation store.Conversation, deltaText string) []StreamEvent {
	format := stringValue(conversation.Metadata["request_format"], "responses")
	switch format {
	case "chat_completions":
		return []StreamEvent{{
			Data: map[string]any{
				"id":     "chatcmpl_" + uuid.NewString(),
				"object": "chat.completion.chunk",
				"model":  stringValue(conversation.Metadata["model"], "chatapi-lab"),
				"choices": []map[string]any{{
					"index": 0,
					"delta": map[string]any{"content": deltaText},
				}},
			},
		}}
	case "anthropic_messages":
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

func BuildStreamComplete(conversation store.Conversation, payload CompletePayload) []StreamEvent {
	format := stringValue(conversation.Metadata["request_format"], "responses")
	switch format {
	case "chat_completions":
		chunk := map[string]any{
			"id":     "chatcmpl_" + uuid.NewString(),
			"object": "chat.completion.chunk",
			"model":  stringValue(conversation.Metadata["model"], "chatapi-lab"),
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "stop",
			}},
		}
		if payload.Mode == "tool_call" {
			chunk["choices"] = []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []map[string]any{{
						"id":   stringValue(payload.ToolCallID, "toolcall_"+uuid.NewString()),
						"type": "function",
						"function": map[string]any{
							"name":      payload.ToolName,
							"arguments": payload.OutputText,
						},
					}},
				},
				"finish_reason": "tool_calls",
			}}
		}
		return []StreamEvent{{Data: chunk}, {Data: "[DONE]", Done: true}}
	case "anthropic_messages":
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
						"stop_reason": "end_turn",
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
				"type":     "response.completed",
				"response": BuildResponse(conversation, payload),
			},
		}}
	}
}

func BuildStreamAbort(conversation store.Conversation, body map[string]any) []StreamEvent {
	format := stringValue(conversation.Metadata["request_format"], "responses")
	switch format {
	case "chat_completions":
		return []StreamEvent{{Data: body}, {Data: "[DONE]", Done: true}}
	case "anthropic_messages":
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
