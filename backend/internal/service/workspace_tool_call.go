package service

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

var ErrToolCallAssistTargetRequired = errors.New("request_id or conversation_id is required")

type ToolCallAssistContext struct {
	Request      store.Request      `json:"request"`
	Conversation store.Conversation `json:"conversation"`
	Messages     []store.Message    `json:"messages"`
	DraftText    string             `json:"draft_text,omitempty"`
	DraftLength  int                `json:"draft_length"`
}

type WorkspaceToolCallService struct {
	store store.Store
}

func NewWorkspaceToolCallService(dataStore store.Store) *WorkspaceToolCallService {
	return &WorkspaceToolCallService{store: dataStore}
}

func (s *WorkspaceToolCallService) AssistSchema() ToolCallAssistSchema {
	return BuildToolCallAssistSchema()
}

func (s *WorkspaceToolCallService) AssistContext(ctx context.Context, ownerID string, requestID string, conversationID string) (ToolCallAssistContext, error) {
	if s == nil || s.store == nil {
		return ToolCallAssistContext{}, ErrForbidden
	}
	ownerID = strings.TrimSpace(ownerID)
	requestID = strings.TrimSpace(requestID)
	conversationID = strings.TrimSpace(conversationID)
	if requestID == "" && conversationID == "" {
		return ToolCallAssistContext{}, ErrToolCallAssistTargetRequired
	}

	request, err := s.resolveRequest(ctx, ownerID, requestID, conversationID)
	if err != nil {
		return ToolCallAssistContext{}, err
	}
	conversation, err := s.store.GetConversation(ctx, request.ConversationID)
	if err != nil {
		return ToolCallAssistContext{}, err
	}
	if ownerID != "" && stringValue(conversation.Metadata["owner_id"], "") != ownerID {
		return ToolCallAssistContext{}, ErrForbidden
	}
	messages, err := s.store.ListMessages(ctx, request.ConversationID)
	if err != nil {
		return ToolCallAssistContext{}, err
	}
	draftText := stringValue(conversation.Metadata["realtime_draft_text"], "")
	return ToolCallAssistContext{
		Request:      request,
		Conversation: conversation,
		Messages:     messages,
		DraftText:    draftText,
		DraftLength:  len([]rune(draftText)),
	}, nil
}

func (s *WorkspaceToolCallService) resolveRequest(ctx context.Context, ownerID string, requestID string, conversationID string) (store.Request, error) {
	if requestID != "" {
		item, err := s.store.GetRequest(ctx, requestID)
		if err != nil {
			return store.Request{}, err
		}
		if ownerID != "" && item.OwnerID != ownerID {
			return store.Request{}, ErrForbidden
		}
		if conversationID != "" && item.ConversationID != conversationID {
			return store.Request{}, store.ErrNotFound
		}
		return item, nil
	}

	items, err := s.store.ListRequests(ctx)
	if err != nil {
		return store.Request{}, err
	}
	for _, item := range items {
		if item.ConversationID != conversationID {
			continue
		}
		if ownerID != "" && item.OwnerID != ownerID {
			return store.Request{}, ErrForbidden
		}
		return item, nil
	}
	return store.Request{}, store.ErrNotFound
}
