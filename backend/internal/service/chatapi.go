package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/store"
)

type ChatAPIService struct {
	store    store.Store
	pending  *PendingRegistry
	realtime *RealtimeHub
}

func NewChatAPIService(dataStore store.Store, pending *PendingRegistry, realtime *RealtimeHub) *ChatAPIService {
	return &ChatAPIService{
		store:    dataStore,
		pending:  pending,
		realtime: realtime,
	}
}

func (s *ChatAPIService) CreatePendingResponse(ctx context.Context, requestFormat string, body map[string]any) (map[string]any, error) {
	model := stringValue(body["model"], "chatapi-lab")
	requestID := "req_" + uuid.NewString()
	responseID := "resp_" + uuid.NewString()
	conversationID := "conv_" + uuid.NewString()
	userContent := extractUserText(body)

	conversation, message, err := s.store.CreatePendingTurn(ctx, store.CreatePendingInput{
		ConversationID: conversationID,
		RequestID:      requestID,
		ResponseID:     responseID,
		RequestFormat:  requestFormat,
		Model:          model,
		UserContent:    userContent,
		RequestBody:    body,
		ToolSchemas:    extractToolSchemas(body),
	})
	if err != nil {
		return nil, err
	}

	s.pending.Add(&PendingTurn{
		RequestID:      requestID,
		ConversationID: conversationID,
		ResponseID:     responseID,
		RequestFormat:  requestFormat,
		Model:          model,
		done:           make(chan PendingResult, 1),
	})
	s.realtime.PublishConversationUpsert(conversation, []store.Message{message})

	result, err := s.pending.Wait(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return result.ResponseBody, nil
}

func (s *ChatAPIService) ListMessages(ctx context.Context, conversationID string) ([]store.Message, error) {
	return s.store.ListMessages(ctx, conversationID)
}

func (s *ChatAPIService) UpdateDraft(ctx context.Context, conversationID string, chunk string) (map[string]any, error) {
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	metadata := conversation.Metadata
	existing, _ := metadata["realtime_draft_text"].(string)
	nextDraft := existing + chunk
	updated, err := s.store.UpdateDraft(ctx, store.UpdateDraftInput{
		ConversationID: conversationID,
		DraftText:      nextDraft,
	})
	if err != nil {
		return nil, err
	}
	s.realtime.PublishConversationUpsert(updated, nil)
	return map[string]any{
		"draft_text":   nextDraft,
		"draft_length": len([]rune(nextDraft)),
	}, nil
}

func (s *ChatAPIService) CompleteConversation(ctx context.Context, input store.CompletePendingInput) (map[string]any, error) {
	conversation, message, err := s.store.CompletePendingTurn(ctx, input)
	if err != nil {
		return nil, err
	}
	messages, err := s.store.ListMessages(ctx, input.ConversationID)
	if err == nil {
		s.realtime.PublishConversationUpsert(conversation, messages)
	} else {
		s.realtime.PublishConversationUpsert(conversation, []store.Message{message})
	}

	responseBody := s.BuildProtocolResponse(
		stringValue(conversation.Metadata["request_format"], "responses"),
		stringValue(conversation.ResponseID, input.ResponseID),
		message.Content,
		input.Mode,
		input.ToolName,
		input.ToolCallID,
		conversation,
	)

	if err := s.pending.Resolve(input.ConversationID, PendingResult{ResponseBody: responseBody}); err != nil {
		return nil, err
	}

	return map[string]any{
		"conversation": conversation,
		"output_text":  message.Content,
	}, nil
}

func (s *ChatAPIService) AbortConversation(ctx context.Context, conversationID string, reason string) error {
	conversation, message, err := s.store.AbortPendingTurn(ctx, store.AbortPendingInput{
		ConversationID: conversationID,
		Reason:         reason,
	})
	if err != nil {
		return err
	}
	messages, listErr := s.store.ListMessages(ctx, conversationID)
	if listErr == nil {
		s.realtime.PublishConversationUpsert(conversation, messages)
	} else {
		s.realtime.PublishConversationUpsert(conversation, []store.Message{message})
	}

	return s.pending.Abort(conversationID, map[string]any{
		"error": map[string]any{
			"message": reason,
			"type":    "request_aborted",
			"code":    "request_aborted",
		},
	})
}

func (s *ChatAPIService) BuildProtocolResponse(format string, responseID string, outputText string, mode string, toolName string, toolCallID string, conversation store.Conversation) map[string]any {
	switch format {
	case "chat_completions":
		message := map[string]any{
			"role":    "assistant",
			"content": outputText,
		}
		if mode == "tool_call" {
			message["content"] = ""
			message["tool_calls"] = []map[string]any{
				{
					"id":   stringValue(toolCallID, "toolcall_"+uuid.NewString()),
					"type": "function",
					"function": map[string]any{
						"name":      toolName,
						"arguments": outputText,
					},
				},
			}
		}
		return map[string]any{
			"id":           stringValue(responseID, "chatcmpl_"+uuid.NewString()),
			"object":       "chat.completion",
			"model":        stringValue(conversation.Metadata["model"], "chatapi-lab"),
			"choices":      []map[string]any{{"index": 0, "message": message, "finish_reason": "stop"}},
			"conversation": conversation,
		}
	case "anthropic_messages":
		content := []map[string]any{{"type": "text", "text": outputText}}
		if mode == "tool_call" {
			content = []map[string]any{{
				"type":  "tool_use",
				"id":    stringValue(toolCallID, "toolu_"+uuid.NewString()),
				"name":  toolName,
				"input": parseJSONValue(outputText),
			}}
		}
		return map[string]any{
			"id":           stringValue(responseID, "msg_"+uuid.NewString()),
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
				{"type": "output_text", "text": outputText},
			},
		}}
		if mode == "tool_call" {
			output = []map[string]any{{
				"type":      "function_call",
				"name":      toolName,
				"call_id":   stringValue(toolCallID, "call_"+uuid.NewString()),
				"arguments": outputText,
			}}
		}
		return map[string]any{
			"id":           responseID,
			"object":       "response",
			"status":       "completed",
			"conversation": conversation,
			"output_text":  outputText,
			"output":       output,
		}
	}
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}

func extractUserText(body map[string]any) string {
	if input, ok := body["input"].(string); ok && strings.TrimSpace(input) != "" {
		return strings.TrimSpace(input)
	}
	if input, ok := body["input"].([]any); ok {
		parts := make([]string, 0)
		for _, item := range input {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			contentItems, ok := record["content"].([]any)
			if !ok {
				continue
			}
			for _, contentItem := range contentItems {
				contentRecord, ok := contentItem.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := contentRecord["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, strings.TrimSpace(text))
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	if messages, ok := body["messages"].([]any); ok {
		for i := len(messages) - 1; i >= 0; i-- {
			record, ok := messages[i].(map[string]any)
			if !ok {
				continue
			}
			if role, _ := record["role"].(string); role != "user" {
				continue
			}
			if content, ok := record["content"].(string); ok && strings.TrimSpace(content) != "" {
				return strings.TrimSpace(content)
			}
		}
	}
	return ""
}

func extractToolSchemas(body map[string]any) []any {
	if tools, ok := body["tools"].([]any); ok {
		return tools
	}
	return nil
}

func parseJSONValue(raw string) any {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	return value
}

func MustConversationID(input map[string]any) (string, error) {
	conversationID, ok := input["conversation_id"].(string)
	if !ok || strings.TrimSpace(conversationID) == "" {
		return "", fmt.Errorf("conversation_id is required")
	}
	return strings.TrimSpace(conversationID), nil
}
