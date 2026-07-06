package protocol

import (
	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/store"
)

func BuildResponse(conversation store.Conversation, result TurnResult) map[string]any {
	meta := ConversationMetaFromConversation(conversation)
	body := BuildResponseForMeta(meta, result)
	body["conversation"] = conversation
	return body
}

func BuildResponseForMeta(meta ConversationMeta, result TurnResult) map[string]any {
	usage := normalizeUsage(result.Usage)
	switch meta.Protocol {
	case ProtocolChatCompletions:
		return map[string]any{
			"id":      chatCompletionID(result),
			"object":  "chat.completion",
			"model":   meta.Model,
			"choices": []map[string]any{{"index": 0, "message": buildChatCompletionMessage(result), "finish_reason": "stop"}},
			"usage": map[string]any{
				"prompt_tokens":     usage.InputTokens,
				"completion_tokens": usage.OutputTokens,
				"total_tokens":      usage.TotalTokens,
			},
		}
	case ProtocolAnthropicMessages:
		return map[string]any{
			"id":          anthropicMessageID(result),
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     buildAnthropicContent(result),
			"usage": map[string]any{
				"input_tokens":  usage.InputTokens,
				"output_tokens": usage.OutputTokens,
			},
		}
	default:
		return map[string]any{
			"id":          responseIDWithFallback(result, "resp_"+uuid.NewString()),
			"object":      "response",
			"status":      "completed",
			"output_text": result.OutputText,
			"usage": map[string]any{
				"input_tokens":  usage.InputTokens,
				"output_tokens": usage.OutputTokens,
				"total_tokens":  usage.TotalTokens,
			},
			"output": buildResponsesOutput(result),
		}
	}
}
