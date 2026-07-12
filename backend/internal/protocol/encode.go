package protocol

import (
	"encoding/json"

	"github.com/google/uuid"
)

func responseIDWithFallback(result TurnResult, fallback string) string {
	return stringValue(result.ResponseID, fallback)
}

func chatCompletionID(result TurnResult) string {
	return responseIDWithFallback(result, "chatcmpl_"+uuid.NewString())
}

func anthropicMessageID(result TurnResult) string {
	return responseIDWithFallback(result, "msg_"+uuid.NewString())
}

func responsesCallID(result TurnResult) string {
	return stringValue(result.ToolCallID, "call_"+uuid.NewString())
}

func openAIToolCallID(result TurnResult) string {
	return stringValue(result.ToolCallID, "toolcall_"+uuid.NewString())
}

func anthropicToolUseID(result TurnResult) string {
	return stringValue(result.ToolCallID, "toolu_"+uuid.NewString())
}

func buildResponsesOutput(result TurnResult) []map[string]any {
	switch result.Mode {
	case "tool_call":
		return []map[string]any{{
			"type":      "function_call",
			"name":      result.ToolName,
			"call_id":   responsesCallID(result),
			"arguments": result.OutputText,
		}}
	case "tool_result":
		return []map[string]any{{
			"type":    "function_call_output",
			"call_id": responsesCallID(result),
			"output":  stringValue(result.ToolOutput, result.OutputText),
		}}
	default:
		return []map[string]any{{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": result.OutputText},
			},
		}}
	}
}

func buildChatCompletionMessage(result TurnResult) map[string]any {
	message := map[string]any{
		"role":    "assistant",
		"content": result.OutputText,
	}
	if result.Mode != "tool_call" {
		return message
	}
	message["content"] = ""
	message["tool_calls"] = []map[string]any{buildChatCompletionToolCall(result)}
	return message
}

func buildChatCompletionToolCall(result TurnResult) map[string]any {
	return map[string]any{
		"id":   openAIToolCallID(result),
		"type": "function",
		"function": map[string]any{
			"name":      result.ToolName,
			"arguments": result.OutputText,
		},
	}
}

func buildAnthropicContent(result TurnResult) []map[string]any {
	if result.Mode != "tool_call" {
		return []map[string]any{{"type": "text", "text": result.OutputText}}
	}
	return []map[string]any{buildAnthropicToolUseBlock(result)}
}

func buildAnthropicToolUseBlock(result TurnResult) map[string]any {
	return map[string]any{
		"type":  "tool_use",
		"id":    anthropicToolUseID(result),
		"name":  result.ToolName,
		"input": parseJSONValue(result.OutputText),
	}
}

func parseJSONValue(raw string) any {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	return value
}

func normalizeUsage(usage Usage) Usage {
	if usage.InputTokens < 0 {
		usage.InputTokens = 0
	}
	if usage.OutputTokens < 0 {
		usage.OutputTokens = 0
	}
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage
}
