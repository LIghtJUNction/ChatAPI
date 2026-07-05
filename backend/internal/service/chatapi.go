package service

import (
	"context"
	"errors"
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
	turn, _, _, err := s.createPendingTurn(ctx, parsed, body)
	if err != nil {
		return nil, err
	}
	result, err := s.pending.Wait(ctx, turn.ConversationID)
	if err != nil {
		return nil, err
	}
	return result.ResponseBody, nil
}

func (s *ChatAPIService) CreatePendingStream(ctx context.Context, requestFormat string, body map[string]any) (*PendingTurn, store.Conversation, error) {
	parsed := protocol.ParseRequest(requestFormat, body)
	turn, conversation, _, err := s.createPendingTurn(ctx, parsed, body)
	if err != nil {
		return nil, store.Conversation{}, err
	}
	return turn, conversation, nil
}

func (s *ChatAPIService) createPendingTurn(ctx context.Context, parsed protocol.ParsedRequest, body map[string]any) (*PendingTurn, store.Conversation, store.Message, error) {
	requestID := "req_" + uuid.NewString()
	responseID := "resp_" + uuid.NewString()
	conversationID := "conv_" + uuid.NewString()

	conversation, message, err := s.store.CreatePendingTurn(ctx, store.CreatePendingInput{
		ConversationID: conversationID,
		RequestID:      requestID,
		ResponseID:     responseID,
		OwnerID:        "lab-user",
		RequestFormat:  parsed.RequestFormat,
		Model:          parsed.Model,
		UserContent:    parsed.UserContent,
		RequestBody:    body,
		ToolSchemas:    parsed.ToolSchemas,
	})
	if err != nil {
		return nil, store.Conversation{}, store.Message{}, err
	}

	turn := &PendingTurn{
		RequestID:      requestID,
		ConversationID: conversationID,
		ResponseID:     responseID,
		RequestFormat:  parsed.RequestFormat,
		Model:          parsed.Model,
		Events:         make(chan PendingEvent, 32),
		done:           make(chan PendingResult, 1),
	}
	s.pending.Add(turn)
	s.realtime.PublishConversationUpsert(conversation, []store.Message{message})
	return turn, conversation, message, nil
}

func (s *ChatAPIService) ListMessages(ctx context.Context, conversationID string) ([]store.Message, error) {
	return s.store.ListMessages(ctx, conversationID)
}

func (s *ChatAPIService) ListMessagesForOwner(ctx context.Context, conversationID string, ownerID string) ([]store.Message, error) {
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if ownerID != "" && stringValue(conversation.Metadata["owner_id"], "") != ownerID {
		return nil, ErrForbidden
	}
	return s.store.ListMessages(ctx, conversationID)
}

func (s *ChatAPIService) ListConversationsForOwner(ctx context.Context, ownerID string) ([]store.Conversation, error) {
	items, err := s.store.ListConversations(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]store.Conversation, 0, len(items))
	for _, item := range items {
		if ownerID == "" || stringValue(item.Metadata["owner_id"], "") == ownerID {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *ChatAPIService) ListRequests(ctx context.Context) ([]store.Request, error) {
	return s.store.ListRequests(ctx)
}

func (s *ChatAPIService) ListRequestsForOwner(ctx context.Context, ownerID string) ([]store.Request, error) {
	items, err := s.store.ListRequests(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]store.Request, 0, len(items))
	for _, item := range items {
		if ownerID == "" || item.OwnerID == ownerID {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *ChatAPIService) GetRequest(ctx context.Context, requestID string) (store.Request, error) {
	return s.store.GetRequest(ctx, requestID)
}

func (s *ChatAPIService) GetRequestForOwner(ctx context.Context, requestID string, ownerID string) (store.Request, error) {
	item, err := s.store.GetRequest(ctx, requestID)
	if err != nil {
		return store.Request{}, err
	}
	if ownerID != "" && item.OwnerID != ownerID {
		return store.Request{}, ErrForbidden
	}
	return item, nil
}

func (s *ChatAPIService) UpdateDraft(ctx context.Context, conversationID string, chunk string) (map[string]any, error) {
	previousState, err := s.pending.StartDelta(conversationID)
	if err != nil {
		return nil, s.resolveTurnMutationError(ctx, conversationID, err)
	}
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		s.pending.RevertFinalize(conversationID, previousState)
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
		s.pending.RevertFinalize(conversationID, previousState)
		return nil, err
	}
	_ = s.pending.Publish(conversationID, PendingEvent{
		Type:      "delta",
		DeltaText: chunk,
	})
	s.realtime.PublishConversationUpsert(updated, nil)
	return map[string]any{
		"draft_text":   nextDraft,
		"draft_length": len([]rune(nextDraft)),
	}, nil
}

func (s *ChatAPIService) CompleteConversation(ctx context.Context, input store.CompletePendingInput) (map[string]any, error) {
	previousState, err := s.pending.StartComplete(input.ConversationID)
	if err != nil {
		return nil, s.resolveTurnMutationError(ctx, input.ConversationID, err)
	}
	conversation, message, err := s.store.CompletePendingTurn(ctx, input)
	if err != nil {
		s.pending.RevertFinalize(input.ConversationID, previousState)
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
		ToolOutput: stringValue(input.ToolOutput, message.Content),
	})
	_ = s.pending.Publish(input.ConversationID, PendingEvent{
		Type:         "complete",
		OutputText:   message.Content,
		Mode:         input.Mode,
		ToolName:     input.ToolName,
		ToolCallID:   input.ToolCallID,
		ToolOutput:   stringValue(input.ToolOutput, message.Content),
		ResponseBody: responseBody,
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
	previousState, err := s.pending.StartAbort(conversationID)
	if err != nil {
		return s.resolveTurnMutationError(ctx, conversationID, err)
	}
	conversation, message, err := s.store.AbortPendingTurn(ctx, store.AbortPendingInput{
		ConversationID: conversationID,
		Reason:         reason,
	})
	if err != nil {
		s.pending.RevertFinalize(conversationID, previousState)
		return err
	}
	messages, listErr := s.store.ListMessages(ctx, conversationID)
	if listErr == nil {
		s.realtime.PublishConversationUpsert(conversation, messages)
	} else {
		s.realtime.PublishConversationUpsert(conversation, []store.Message{message})
	}

	body := map[string]any{
		"error": map[string]any{
			"message": reason,
			"type":    "request_aborted",
			"code":    "request_aborted",
		},
	}
	_ = s.pending.Publish(conversationID, PendingEvent{
		Type:      "abort",
		ErrorBody: body,
	})

	return s.pending.Abort(conversationID, body)
}

func (s *ChatAPIService) resolveTurnMutationError(ctx context.Context, conversationID string, err error) error {
	if !errors.Is(err, ErrPendingNotFound) {
		return err
	}
	conversation, getErr := s.store.GetConversation(ctx, conversationID)
	if getErr != nil {
		return err
	}
	status := stringValue(conversation.Metadata["realtime_status"], "")
	if status == "closed" || status == "aborted" || status == "expired" {
		return ErrPendingConflict
	}
	return err
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
