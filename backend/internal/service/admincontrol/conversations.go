package admincontrol

import (
	"context"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

func (s *Service) ListConversations(ctx context.Context) ([]common.Conversation, error) {
	return s.chatStore.ListConversations(ctx)
}

func (s *Service) ListMessages(ctx context.Context, conversationID string) ([]common.Message, error) {
	return s.query.ListMessages(ctx, strings.TrimSpace(conversationID))
}

func (s *Service) AbortConversation(ctx context.Context, conversationID string, reason string) (map[string]any, error) {
	result, err := s.control.Execute(ctx, controlsvc.Command{
		Kind:           turnsvc.TurnControlAbort,
		ConversationID: strings.TrimSpace(conversationID),
		AbortReason:    strings.TrimSpace(reason),
	})
	return result.Body, err
}

func (s *Service) CompleteConversation(ctx context.Context, conversationID string, text string, mode string, toolName string, toolCallID string, toolOutput string) (map[string]any, error) {
	result, err := s.control.Execute(ctx, controlsvc.Command{
		Kind:           turnsvc.TurnControlStreamComplete,
		ConversationID: strings.TrimSpace(conversationID),
		OutputText:     text,
		Mode:           mode,
		ToolName:       toolName,
		ToolCallID:     toolCallID,
		ToolOutput:     toolOutput,
	})
	return result.Body, err
}

func (s *Service) DeleteConversation(ctx context.Context, conversationID string) (common.DeleteConversationsResult, error) {
	return s.chatStore.DeleteConversations(ctx, []string{strings.TrimSpace(conversationID)})
}
