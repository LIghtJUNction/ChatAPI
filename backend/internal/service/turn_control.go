package service

import (
	"context"
	"fmt"

	"github.com/zyf/chatapi/internal/store"
)

type TurnControlKind string

const (
	TurnControlRespond        TurnControlKind = "respond"
	TurnControlStreamDelta    TurnControlKind = "stream_delta"
	TurnControlStreamComplete TurnControlKind = "stream_complete"
	TurnControlAbort          TurnControlKind = "abort"
)

type TurnControlCommand struct {
	Kind                TurnControlKind
	ConversationID      string
	ResponseID          string
	OutputText          string
	Mode                string
	ToolName            string
	ToolCallID          string
	ToolOutput          string
	ReasoningStreamMode string
	AbortReason         string
}

func (c TurnControlCommand) Validate() error {
	if c.ConversationID == "" {
		return fmt.Errorf("conversation_id is required")
	}
	switch c.Kind {
	case TurnControlRespond, TurnControlStreamComplete:
		return nil
	case TurnControlStreamDelta:
		return nil
	case TurnControlAbort:
		if c.AbortReason == "" {
			return fmt.Errorf("error is required")
		}
		return nil
	default:
		return fmt.Errorf("unsupported turn control kind: %s", c.Kind)
	}
}

func (s *ChatAPIService) ExecuteTurnControl(ctx context.Context, command TurnControlCommand) (map[string]any, error) {
	if err := command.Validate(); err != nil {
		return nil, err
	}

	switch command.Kind {
	case TurnControlRespond, TurnControlStreamComplete:
		return s.CompleteConversation(ctx, store.CompletePendingInput{
			ConversationID:      command.ConversationID,
			ResponseID:          command.ResponseID,
			OutputText:          command.OutputText,
			Mode:                command.Mode,
			ToolName:            command.ToolName,
			ToolCallID:          command.ToolCallID,
			ToolOutput:          command.ToolOutput,
			ReasoningStreamMode: command.ReasoningStreamMode,
		})
	case TurnControlStreamDelta:
		return s.UpdateDraft(ctx, command.ConversationID, command.OutputText)
	case TurnControlAbort:
		if err := s.AbortConversation(ctx, command.ConversationID, command.AbortReason); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	default:
		return nil, fmt.Errorf("unsupported turn control kind: %s", command.Kind)
	}
}

func (s *ChatAPIService) ExecuteTurnControlByRequestID(ctx context.Context, requestID string, command TurnControlCommand) (map[string]any, error) {
	request, err := s.store.GetRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	command.ConversationID = request.ConversationID
	return s.ExecuteTurnControl(ctx, command)
}
