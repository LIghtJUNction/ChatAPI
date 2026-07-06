package protocol

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/store"
)

func BuildResponse(conversation store.Conversation, result TurnResult) map[string]any {
	meta := ConversationMetaFromConversation(conversation)
	switch meta.Protocol {
	case ProtocolChatCompletions:
		message := map[string]any{
			"role":    "assistant",
			"content": result.OutputText,
		}
		if result.Mode == "tool_call" {
			message["content"] = ""
			message["tool_calls"] = []map[string]any{
				{
					"id":   stringValue(result.ToolCallID, "toolcall_"+uuid.NewString()),
					"type": "function",
					"function": map[string]any{
						"name":      result.ToolName,
						"arguments": result.OutputText,
					},
				},
			}
		}
		return map[string]any{
			"id":           stringValue(result.ResponseID, "chatcmpl_"+uuid.NewString()),
			"object":       "chat.completion",
			"model":        meta.Model,
			"choices":      []map[string]any{{"index": 0, "message": message, "finish_reason": "stop"}},
			"conversation": conversation,
		}
	case ProtocolAnthropicMessages:
		content := []map[string]any{{"type": "text", "text": result.OutputText}}
		if result.Mode == "tool_call" {
			content = []map[string]any{{
				"type":  "tool_use",
				"id":    stringValue(result.ToolCallID, "toolu_"+uuid.NewString()),
				"name":  result.ToolName,
				"input": parseJSONValue(result.OutputText),
			}}
		}
		return map[string]any{
			"id":           stringValue(result.ResponseID, "msg_"+uuid.NewString()),
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
				{"type": "output_text", "text": result.OutputText},
			},
		}}
		if result.Mode == "tool_call" {
			output = []map[string]any{{
				"type":      "function_call",
				"name":      result.ToolName,
				"call_id":   stringValue(result.ToolCallID, "call_"+uuid.NewString()),
				"arguments": result.OutputText,
			}}
		} else if result.Mode == "tool_result" {
			output = []map[string]any{{
				"type":    "function_call_output",
				"call_id": stringValue(result.ToolCallID, "call_"+uuid.NewString()),
				"output":  stringValue(result.ToolOutput, result.OutputText),
			}}
		}
		return map[string]any{
			"id":           result.ResponseID,
			"object":       "response",
			"status":       "completed",
			"conversation": conversation,
			"output_text":  result.OutputText,
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
