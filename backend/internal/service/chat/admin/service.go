package admin

import (
	"context"
	"strings"

	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
	turnquerysvc "github.com/zyf/chatapi/internal/service/chat/turnquery"
	"github.com/zyf/chatapi/internal/store"
)

type Service struct {
	Query *turnquerysvc.Service
	Turn  *turnsvc.Service
	Store store.Store
}

func NewService(query *turnquerysvc.Service, turn *turnsvc.Service, dataStore store.Store) *Service {
	return &Service{Query: query, Turn: turn, Store: dataStore}
}

func (s *Service) ListRequests(ctx context.Context) ([]store.Request, error) {
	return s.Query.ListRequests(ctx)
}

func (s *Service) GetRequest(ctx context.Context, requestID string) (store.Request, error) {
	return s.Query.GetRequest(ctx, strings.TrimSpace(requestID))
}

func (s *Service) ListConversations(ctx context.Context) ([]store.Conversation, error) {
	return s.Store.ListConversations(ctx)
}

func (s *Service) ListMessages(ctx context.Context, conversationID string) ([]store.Message, error) {
	return s.Query.ListMessages(ctx, strings.TrimSpace(conversationID))
}

func (s *Service) AbortConversation(ctx context.Context, conversationID string, reason string) (map[string]any, error) {
	return s.Turn.ExecuteTurnControl(ctx, turnsvc.TurnControlCommand{
		Kind:           turnsvc.TurnControlAbort,
		ConversationID: strings.TrimSpace(conversationID),
		AbortReason:    strings.TrimSpace(reason),
	})
}

func (s *Service) CompleteConversation(ctx context.Context, conversationID string, text string, mode string, toolName string, toolCallID string, toolOutput string) (map[string]any, error) {
	return s.Turn.ExecuteTurnControl(ctx, turnsvc.TurnControlCommand{
		Kind:           turnsvc.TurnControlStreamComplete,
		ConversationID: strings.TrimSpace(conversationID),
		OutputText:     text,
		Mode:           mode,
		ToolName:       toolName,
		ToolCallID:     toolCallID,
		ToolOutput:     toolOutput,
	})
}

func (s *Service) AbortByRequest(ctx context.Context, requestID string, reason string) (map[string]any, error) {
	return s.Turn.ExecuteTurnControlByRequestID(ctx, strings.TrimSpace(requestID), turnsvc.TurnControlCommand{
		Kind:        turnsvc.TurnControlAbort,
		AbortReason: strings.TrimSpace(reason),
	})
}

func (s *Service) CompleteByRequest(ctx context.Context, requestID string, text string, mode string, toolName string, toolCallID string, toolOutput string) (map[string]any, error) {
	return s.Turn.ExecuteTurnControlByRequestID(ctx, strings.TrimSpace(requestID), turnsvc.TurnControlCommand{
		Kind:       turnsvc.TurnControlStreamComplete,
		OutputText: text,
		Mode:       mode,
		ToolName:   toolName,
		ToolCallID: toolCallID,
		ToolOutput: toolOutput,
	})
}

func (s *Service) DeleteConversation(ctx context.Context, conversationID string) (store.DeleteConversationsResult, error) {
	return s.Store.DeleteConversations(ctx, []string{strings.TrimSpace(conversationID)})
}
