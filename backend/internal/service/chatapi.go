package service

import (
	"context"
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

	responseBody := map[string]any{
		"id":           input.ResponseID,
		"object":       "response",
		"status":       "completed",
		"conversation": conversation,
		"output_text":  input.OutputText,
		"output": []map[string]any{
			{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": input.OutputText},
				},
			},
		},
	}

	if err := s.pending.Resolve(input.ConversationID, PendingResult{ResponseBody: responseBody}); err != nil {
		return nil, err
	}

	return map[string]any{
		"conversation": conversation,
		"output_text":  input.OutputText,
	}, nil
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

func MustConversationID(input map[string]any) (string, error) {
	conversationID, ok := input["conversation_id"].(string)
	if !ok || strings.TrimSpace(conversationID) == "" {
		return "", fmt.Errorf("conversation_id is required")
	}
	return strings.TrimSpace(conversationID), nil
}
