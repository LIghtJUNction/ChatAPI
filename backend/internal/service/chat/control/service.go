package control

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	pendingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	"go.uber.org/zap"
)

type Query interface {
	ListMessagesForOwner(context.Context, string, string) ([]common.Message, error)
}

type Turn interface {
	ExecuteTurnControl(context.Context, turnsvc.TurnControlCommand) (map[string]any, error)
}

type Service struct {
	Query  Query
	Turn   Turn
	Logger *zap.Logger
}

type Command struct {
	OwnerID             string
	Kind                turnsvc.TurnControlKind
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

type Result struct {
	Body map[string]any
}

type ErrorKind string

const (
	ErrorKindInvalid     ErrorKind = "invalid"
	ErrorKindForbidden   ErrorKind = "forbidden"
	ErrorKindNotFound    ErrorKind = "not_found"
	ErrorKindConflict    ErrorKind = "conflict"
	ErrorKindUnavailable ErrorKind = "unavailable"
	ErrorKindInternal    ErrorKind = "internal"
)

type Error struct {
	Kind    ErrorKind
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(query Query, turn Turn, logger *zap.Logger) *Service {
	return &Service{Query: query, Turn: turn, Logger: logger}
}

func (s *Service) Execute(ctx context.Context, command Command) (Result, error) {
	turnCommand := command.TurnCommand()
	if err := turnCommand.Validate(); err != nil {
		return Result{}, s.controlError(ctx, turnCommand, err, ErrorKindInvalid, "invalid_turn_control_command")
	}
	ownerID := strings.TrimSpace(command.OwnerID)
	if ownerID != "" && s.Query != nil {
		if _, err := s.Query.ListMessagesForOwner(ctx, strings.TrimSpace(turnCommand.ConversationID), ownerID); err != nil {
			kind := ErrorKindNotFound
			code := "turn_control_conversation_not_found"
			if errors.Is(err, turnquerysvc.ErrForbidden) {
				kind = ErrorKindForbidden
				code = "turn_control_forbidden"
			}
			return Result{}, s.controlError(ctx, turnCommand, err, kind, code)
		}
	}
	if s.Turn == nil {
		return Result{}, s.controlError(ctx, turnCommand, errors.New("turn control executor unavailable"), ErrorKindUnavailable, "turn_control_unavailable")
	}
	body, err := s.Turn.ExecuteTurnControl(ctx, turnCommand)
	if err != nil {
		return Result{}, s.mapExecutionError(ctx, turnCommand, err)
	}
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", ownerID),
		zap.String("conversation.id", turnCommand.ConversationID),
		zap.String("turn.control.kind", string(turnCommand.Kind)),
	).Info("turn control executed")
	return Result{Body: body}, nil
}

func (s *Service) mapExecutionError(ctx context.Context, command turnsvc.TurnControlCommand, err error) error {
	switch {
	case errors.Is(err, pendingsvc.ErrPendingConflict), errors.Is(err, common.ErrTurnConflict):
		return s.controlError(ctx, command, err, ErrorKindConflict, "turn_control_conflict")
	case errors.Is(err, pendingsvc.ErrPendingNotFound), errors.Is(err, common.ErrNotFound):
		return s.controlError(ctx, command, err, ErrorKindNotFound, "turn_control_not_found")
	default:
		return s.controlError(ctx, command, err, ErrorKindInternal, "turn_control_failed")
	}
}

func (s *Service) controlError(ctx context.Context, command turnsvc.TurnControlCommand, err error, kind ErrorKind, code string) error {
	level := logging.BindContext(s.Logger, ctx,
		zap.String("conversation.id", command.ConversationID),
		zap.String("turn.control.kind", string(command.Kind)),
		zap.String("error.kind", string(kind)),
		zap.String("error.code", code),
	)
	if kind == ErrorKindInternal || kind == ErrorKindUnavailable {
		level.Error("turn control failed", zap.Error(err))
	} else {
		level.Warn("turn control rejected", zap.Error(err))
	}
	message := code
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return &Error{Kind: kind, Code: code, Message: message, Err: err}
}

func (c Command) TurnCommand() turnsvc.TurnControlCommand {
	mode := strings.TrimSpace(c.Mode)
	if mode == "" {
		mode = "assistant_message"
	}
	output := strings.TrimSpace(c.ToolOutput)
	if output == "" {
		output = strings.TrimSpace(c.OutputText)
	}
	return turnsvc.TurnControlCommand{
		Kind:                c.Kind,
		ConversationID:      strings.TrimSpace(c.ConversationID),
		ResponseID:          strings.TrimSpace(c.ResponseID),
		OutputText:          strings.TrimSpace(c.OutputText),
		Mode:                mode,
		ToolName:            strings.TrimSpace(c.ToolName),
		ToolCallID:          strings.TrimSpace(c.ToolCallID),
		ToolOutput:          output,
		ReasoningStreamMode: strings.TrimSpace(c.ReasoningStreamMode),
		AbortReason:         strings.TrimSpace(c.AbortReason),
	}
}
