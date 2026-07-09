package admincontrol

import (
	"context"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

func (s *Service) ListRequests(ctx context.Context) ([]common.Request, error) {
	return s.query.ListRequests(ctx)
}

func (s *Service) GetRequest(ctx context.Context, requestID string) (common.Request, error) {
	return s.query.GetRequest(ctx, strings.TrimSpace(requestID))
}

func (s *Service) AbortByRequest(ctx context.Context, requestID string, reason string) (map[string]any, error) {
	request, err := s.query.GetRequest(ctx, strings.TrimSpace(requestID))
	if err != nil {
		return nil, err
	}
	result, err := s.control.Execute(ctx, controlsvc.Command{
		Kind:           turnsvc.TurnControlAbort,
		ConversationID: strings.TrimSpace(request.ConversationID),
		AbortReason:    strings.TrimSpace(reason),
	})
	return result.Body, err
}

func (s *Service) CompleteByRequest(ctx context.Context, requestID string, text string, mode string, toolName string, toolCallID string, toolOutput string) (map[string]any, error) {
	request, err := s.query.GetRequest(ctx, strings.TrimSpace(requestID))
	if err != nil {
		return nil, err
	}
	result, err := s.control.Execute(ctx, controlsvc.Command{
		Kind:           turnsvc.TurnControlStreamComplete,
		ConversationID: strings.TrimSpace(request.ConversationID),
		OutputText:     text,
		Mode:           mode,
		ToolName:       toolName,
		ToolCallID:     toolCallID,
		ToolOutput:     toolOutput,
	})
	return result.Body, err
}
