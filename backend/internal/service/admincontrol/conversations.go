package admincontrol

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/repository/common"
	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
)

func (s *Service) ListConversations(ctx context.Context) ([]common.Conversation, error) {
	return s.chatStore.ListConversations(ctx)
}

func (s *Service) ListMessages(ctx context.Context, conversationID string) ([]common.Message, error) {
	return s.query.ListMessages(ctx, strings.TrimSpace(conversationID))
}

func (s *Service) AbortConversation(ctx context.Context, conversationID string, reason string) (map[string]any, error) {
	return s.turn.ExecuteTurnControl(ctx, turnsvc.TurnControlCommand{
		Kind:           turnsvc.TurnControlAbort,
		ConversationID: strings.TrimSpace(conversationID),
		AbortReason:    strings.TrimSpace(reason),
	})
}

func (s *Service) CompleteConversation(ctx context.Context, conversationID string, text string, mode string, toolName string, toolCallID string, toolOutput string) (map[string]any, error) {
	return s.turn.ExecuteTurnControl(ctx, turnsvc.TurnControlCommand{
		Kind:           turnsvc.TurnControlStreamComplete,
		ConversationID: strings.TrimSpace(conversationID),
		OutputText:     text,
		Mode:           mode,
		ToolName:       toolName,
		ToolCallID:     toolCallID,
		ToolOutput:     toolOutput,
	})
}

func (s *Service) DeleteConversation(ctx context.Context, conversationID string) (common.DeleteConversationsResult, error) {
	return s.chatStore.DeleteConversations(ctx, []string{strings.TrimSpace(conversationID)})
}
