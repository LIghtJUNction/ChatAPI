package protocol

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/store"
)

type CompletePayload struct {
	ResponseID string
	OutputText string
	Mode       string
	ToolName   string
	ToolCallID string
}

func BuildResponse(conversation store.Conversation, payload CompletePayload) map[string]any {
	format := stringValue(conversation.Metadata["request_format"], "responses")
	switch format {
	case "chat_completions":
		message := map[string]any{
			"role":    "assistant",
			"content": payload.OutputText,
		}
		if payload.Mode == "tool_call" {
			message["content"] = ""
			message["tool_calls"] = []map[string]any{
				{
					"id":   stringValue(payload.ToolCallID, "toolcall_"+uuid.NewString()),
					"type": "function",
					"function": map[string]any{
						"name":      payload.ToolName,
						"arguments": payload.OutputText,
					},
				},
			}
		}
		return map[string]any{
			"id":           stringValue(payload.ResponseID, "chatcmpl_"+uuid.NewString()),
			"object":       "chat.completion",
			"model":        stringValue(conversation.Metadata["model"], "chatapi-lab"),
			"choices":      []map[string]any{{"index": 0, "message": message, "finish_reason": "stop"}},
			"conversation": conversation,
		}
	case "anthropic_messages":
		content := []map[string]any{{"type": "text", "text": payload.OutputText}}
		if payload.Mode == "tool_call" {
			content = []map[string]any{{
				"type":  "tool_use",
				"id":    stringValue(payload.ToolCallID, "toolu_"+uuid.NewString()),
				"name":  payload.ToolName,
				"input": parseJSONValue(payload.OutputText),
			}}
		}
		return map[string]any{
			"id":           stringValue(payload.ResponseID, "msg_"+uuid.NewString()),
			"type":         "message",
			"role":         "assistant",
			"stop_reason":  "end_turn",
			"content":      content,
			"conversation": conversation,
		}
	default:
		output := []map[string]any{{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": payload.OutputText},
			},
		}}
		if payload.Mode == "tool_call" {
			output = []map[string]any{{
				"type":      "function_call",
				"name":      payload.ToolName,
				"call_id":   stringValue(payload.ToolCallID, "call_"+uuid.NewString()),
				"arguments": payload.OutputText,
			}}
		}
		return map[string]any{
			"id":           payload.ResponseID,
			"object":       "response",
			"status":       "completed",
			"conversation": conversation,
			"output_text":  payload.OutputText,
			"output":       output,
		}
	}
}

func parseJSONValue(raw string) any {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	return value
}
