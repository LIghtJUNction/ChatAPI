package admincontrol

import (
	"context"
	"strings"

	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
	"github.com/zyf/chatapi/internal/store"
)

func (s *Service) ListRequests(ctx context.Context) ([]store.Request, error) {
	return s.query.ListRequests(ctx)
}

func (s *Service) GetRequest(ctx context.Context, requestID string) (store.Request, error) {
	return s.query.GetRequest(ctx, strings.TrimSpace(requestID))
}

func (s *Service) AbortByRequest(ctx context.Context, requestID string, reason string) (map[string]any, error) {
	return s.turn.ExecuteTurnControlByRequestID(ctx, strings.TrimSpace(requestID), turnsvc.TurnControlCommand{
		Kind:        turnsvc.TurnControlAbort,
		AbortReason: strings.TrimSpace(reason),
	})
}

func (s *Service) CompleteByRequest(ctx context.Context, requestID string, text string, mode string, toolName string, toolCallID string, toolOutput string) (map[string]any, error) {
	return s.turn.ExecuteTurnControlByRequestID(ctx, strings.TrimSpace(requestID), turnsvc.TurnControlCommand{
		Kind:       turnsvc.TurnControlStreamComplete,
		OutputText: text,
		Mode:       mode,
		ToolName:   toolName,
		ToolCallID: toolCallID,
		ToolOutput: toolOutput,
	})
}
