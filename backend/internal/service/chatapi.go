package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/protocol"
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
	parsed := protocol.ParseRequest(requestFormat, body)
	requestID := "req_" + uuid.NewString()
	responseID := "resp_" + uuid.NewString()
	conversationID := "conv_" + uuid.NewString()

	conversation, message, err := s.store.CreatePendingTurn(ctx, store.CreatePendingInput{
		ConversationID: conversationID,
		RequestID:      requestID,
		ResponseID:     responseID,
		RequestFormat:  parsed.RequestFormat,
		Model:          parsed.Model,
		UserContent:    parsed.UserContent,
		RequestBody:    body,
		ToolSchemas:    parsed.ToolSchemas,
	})
	if err != nil {
		return nil, err
	}

	s.pending.Add(&PendingTurn{
		RequestID:      requestID,
		ConversationID: conversationID,
		ResponseID:     responseID,
		RequestFormat:  parsed.RequestFormat,
		Model:          parsed.Model,
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

	responseBody := protocol.BuildResponse(conversation, protocol.CompletePayload{
		ResponseID: stringValue(conversation.ResponseID, input.ResponseID),
		OutputText: message.Content,
		Mode:       input.Mode,
		ToolName:   input.ToolName,
		ToolCallID: input.ToolCallID,
	})

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

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}

func MustConversationID(input map[string]any) (string, error) {
	conversationID, ok := input["conversation_id"].(string)
	if !ok || strings.TrimSpace(conversationID) == "" {
		return "", fmt.Errorf("conversation_id is required")
	}
	return strings.TrimSpace(conversationID), nil
}
