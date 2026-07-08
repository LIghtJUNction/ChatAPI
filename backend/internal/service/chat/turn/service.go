package turn

import (
	"context"
	"errors"
	"time"

	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/protocol"
	"github.com/zyf/chatapi/internal/repository/chat"
	"github.com/zyf/chatapi/internal/repository/common"
	"go.uber.org/zap"
)

var ErrPendingNotFound = errors.New("pending turn not found")
var ErrPendingConflict = errors.New("pending turn already finalized")

type MutationErrorResolver func(context.Context, string, error) error
type TextNotifier func(context.Context, string, string, string)
type AdmissionHook func(context.Context, string) error

type ExpireResult struct {
	ExpiredConversations int `json:"expired_conversations"`
	ExpiredActiveTurns   int `json:"expired_active_turns"`
}

type Service struct {
	Submitter                   *Submitter
	Pending                     pendingRegistryLike
	Store                       chat.Store
	ResolveMutationError        MutationErrorResolver
	NotifyText                  TextNotifier
	EnsureMessageAdmission      AdmissionHook
	EnsureConversationAdmission AdmissionHook
	OwnerIDFromContext          func(context.Context) string
	ActorFromContext            func(context.Context) (actor.Actor, bool)
	Logger                      *zap.Logger
}

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
		return errors.New("conversation_id is required")
	}
	switch c.Kind {
	case TurnControlRespond, TurnControlStreamComplete, TurnControlStreamDelta:
		return nil
	case TurnControlAbort:
		if c.AbortReason == "" {
			return errors.New("error is required")
		}
		return nil
	default:
		return errors.New("unsupported turn control kind: " + string(c.Kind))
	}
}

func (s *Service) CreatePendingResponse(ctx context.Context, input SubmitInput) (map[string]any, error) {
	ownerID := s.ownerID(ctx)
	if err := s.ensureAdmissions(ctx, ownerID); err != nil {
		return nil, err
	}
	input.OwnerID = ownerID
	input.Actor = s.actor(ctx)
	turn, _, _, err := s.Submitter.Submit(ctx, input)
	if err != nil {
		logging.BindContext(s.Logger, ctx, zap.String("owner.id", ownerID)).Error("create pending response submit failed", zap.Error(err))
		return nil, err
	}
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", ownerID),
		zap.String("conversation.id", turn.ConversationID),
		zap.String("request.id", turn.RequestID),
	).Info("pending response created")
	result, err := s.Pending.WaitTurn(ctx, turn)
	if err != nil {
		logging.BindContext(s.Logger, ctx,
			zap.String("owner.id", ownerID),
			zap.String("conversation.id", turn.ConversationID),
			zap.String("request.id", turn.RequestID),
		).Warn("pending response wait interrupted", zap.Error(err))
		return nil, err
	}
	return result.ResponseBody, nil
}

func (s *Service) CreatePendingStream(ctx context.Context, input SubmitInput) (*PendingTurn, common.Conversation, error) {
	ownerID := s.ownerID(ctx)
	if err := s.ensureAdmissions(ctx, ownerID); err != nil {
		return nil, common.Conversation{}, err
	}
	input.OwnerID = ownerID
	input.Actor = s.actor(ctx)
	turn, conversation, _, err := s.Submitter.Submit(ctx, input)
	if err != nil {
		logging.BindContext(s.Logger, ctx, zap.String("owner.id", ownerID)).Error("create pending stream submit failed", zap.Error(err))
		return nil, common.Conversation{}, err
	}
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", ownerID),
		zap.String("conversation.id", conversation.ID),
		zap.String("request.id", turn.RequestID),
	).Info("pending stream created")
	return turn, conversation, nil
}

func (s *Service) UpdateDraft(ctx context.Context, conversationID string, chunk string) (map[string]any, error) {
	previousState, err := s.Pending.StartDelta(conversationID)
	if err != nil {
		return nil, s.resolveMutationError(ctx, conversationID, err)
	}
	conversation, err := s.Store.GetConversation(ctx, conversationID)
	if err != nil {
		s.Pending.RevertFinalize(conversationID, previousState)
		return nil, err
	}
	metadata := conversation.Metadata
	existing, _ := metadata["realtime_draft_text"].(string)
	nextDraft := existing + chunk
	updated, err := s.Store.UpdateDraft(ctx, common.UpdateDraftInput{
		ConversationID: conversationID,
		DraftText:      nextDraft,
	})
	if err != nil {
		s.Pending.RevertFinalize(conversationID, previousState)
		return nil, err
	}
	_ = s.Pending.Publish(conversationID, PendingEvent{Type: "delta", DeltaText: chunk})
	s.Submitter.Realtime.PublishConversationUpsert(updated, nil)
	s.notifyText(ctx, s.ownerID(ctx), updated.Title, chunk)
	return map[string]any{"draft_text": nextDraft, "draft_length": len([]rune(nextDraft))}, nil
}

func (s *Service) CompleteConversation(ctx context.Context, input common.CompletePendingInput) (map[string]any, error) {
	previousState, err := s.Pending.StartComplete(input.ConversationID)
	if err != nil {
		return nil, s.resolveMutationError(ctx, input.ConversationID, err)
	}
	conversation, message, err := s.Store.CompletePendingTurn(ctx, input)
	if err != nil {
		s.Pending.RevertFinalize(input.ConversationID, previousState)
		return nil, err
	}
	messages, err := s.Store.ListMessages(ctx, input.ConversationID)
	if err == nil {
		s.Submitter.Realtime.PublishConversationUpsert(conversation, messages)
	} else {
		s.Submitter.Realtime.PublishConversationUpsert(conversation, []common.Message{message})
	}
	responseBody := protocol.BuildResponseForMeta(protocol.ConversationMeta{
		Protocol:   protocol.ParseProtocol(stringValue(conversation.Metadata["request_format"], string(protocol.ProtocolResponses))),
		Model:      stringValue(conversation.Metadata["model"], "chatapi-lab"),
		ResponseID: stringValue(conversation.ResponseID, input.ResponseID),
	}, protocol.TurnResult{
		ResponseID: stringValue(conversation.ResponseID, input.ResponseID),
		OutputText: message.Content,
		Mode:       input.Mode,
		ToolName:   input.ToolName,
		ToolCallID: input.ToolCallID,
		ToolOutput: stringValue(input.ToolOutput, message.Content),
	})
	_ = s.Pending.Publish(input.ConversationID, PendingEvent{
		Type:         "complete",
		OutputText:   message.Content,
		Mode:         input.Mode,
		ToolName:     input.ToolName,
		ToolCallID:   input.ToolCallID,
		ToolOutput:   stringValue(input.ToolOutput, message.Content),
		ResponseBody: responseBody,
	})
	if err := s.Pending.Resolve(input.ConversationID, PendingResult{ResponseBody: responseBody}); err != nil {
		return nil, err
	}
	s.notifyText(ctx, s.ownerID(ctx), conversation.Title, message.Content)
	return map[string]any{"conversation": conversation, "output_text": message.Content}, nil
}

func (s *Service) AbortConversation(ctx context.Context, conversationID string, reason string) error {
	previousState, err := s.Pending.StartAbort(conversationID)
	if err != nil {
		logging.BindContext(s.Logger, ctx,
			zap.String("conversation.id", conversationID),
			zap.String("turn.action", "abort"),
		).Warn("turn abort start rejected", zap.Error(err))
		return s.resolveMutationError(ctx, conversationID, err)
	}
	conversation, message, err := s.Store.AbortPendingTurn(ctx, common.AbortPendingInput{ConversationID: conversationID, Reason: reason})
	if err != nil {
		s.Pending.RevertFinalize(conversationID, previousState)
		logging.BindContext(s.Logger, ctx,
			zap.String("conversation.id", conversationID),
			zap.String("turn.action", "abort"),
		).Error("turn abort persistence failed", zap.Error(err))
		return err
	}
	messages, listErr := s.Store.ListMessages(ctx, conversationID)
	if listErr == nil {
		s.Submitter.Realtime.PublishConversationUpsert(conversation, messages)
	} else {
		s.Submitter.Realtime.PublishConversationUpsert(conversation, []common.Message{message})
	}
	body := protocol.AbortError(stringValue(conversation.Metadata["request_format"], string(protocol.ProtocolResponses)), reason)
	_ = s.Pending.Publish(conversationID, PendingEvent{Type: "abort", ErrorBody: body})
	s.notifyText(ctx, s.ownerID(ctx), conversation.Title, reason)
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", s.ownerID(ctx)),
		zap.String("conversation.id", conversationID),
		zap.String("request.format", stringValue(conversation.Metadata["request_format"], string(protocol.ProtocolResponses))),
		zap.String("turn.action", "abort"),
	).Info("turn aborted conversation")
	return s.Pending.Abort(conversationID, body)
}

func (s *Service) ExpirePendingTurns(ctx context.Context, ttl time.Duration, now time.Time) (ExpireResult, error) {
	if ttl <= 0 {
		return ExpireResult{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.UTC().Add(-ttl)
	body := pendingExpiredBody(ttl)
	activeExpired := s.Pending.ExpireOlderThan(cutoff, body)
	dbResult, err := s.Store.ExpirePendingTurns(ctx, cutoff)
	if err != nil {
		return ExpireResult{}, err
	}
	return ExpireResult{ExpiredConversations: dbResult.ExpiredConversations, ExpiredActiveTurns: activeExpired}, nil
}

func (s *Service) ExecuteTurnControl(ctx context.Context, command TurnControlCommand) (map[string]any, error) {
	if err := command.Validate(); err != nil {
		logging.BindContext(s.Logger, ctx,
			zap.String("conversation.id", command.ConversationID),
			zap.String("turn.control.kind", string(command.Kind)),
		).Warn("turn control validation failed", zap.Error(err))
		return nil, err
	}
	logging.BindContext(s.Logger, ctx,
		zap.String("conversation.id", command.ConversationID),
		zap.String("turn.control.kind", string(command.Kind)),
		zap.String("response.id", command.ResponseID),
	).Debug("turn control dispatch")
	switch command.Kind {
	case TurnControlRespond, TurnControlStreamComplete:
		return s.CompleteConversation(ctx, common.CompletePendingInput{
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
		return nil, errors.New("unsupported turn control kind: " + string(command.Kind))
	}
}

func (s *Service) ExecuteTurnControlByRequestID(ctx context.Context, requestID string, command TurnControlCommand) (map[string]any, error) {
	request, err := s.Store.GetRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	command.ConversationID = request.ConversationID
	return s.ExecuteTurnControl(ctx, command)
}

func (s *Service) ensureAdmissions(ctx context.Context, ownerID string) error {
	if s.EnsureMessageAdmission != nil {
		if err := s.EnsureMessageAdmission(ctx, ownerID); err != nil {
			return err
		}
	}
	if s.EnsureConversationAdmission != nil {
		if err := s.EnsureConversationAdmission(ctx, ownerID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ownerID(ctx context.Context) string {
	if s.OwnerIDFromContext == nil {
		return ""
	}
	return s.OwnerIDFromContext(ctx)
}

func (s *Service) actor(ctx context.Context) actor.Actor {
	if s.ActorFromContext == nil {
		return actor.Actor{}
	}
	value, ok := s.ActorFromContext(ctx)
	if !ok {
		return actor.Actor{}
	}
	return value
}

func (s *Service) notifyText(ctx context.Context, ownerID string, title string, text string) {
	if s.NotifyText != nil {
		s.NotifyText(ctx, ownerID, title, text)
	}
}

func (s *Service) resolveMutationError(ctx context.Context, conversationID string, err error) error {
	if s.ResolveMutationError == nil {
		return err
	}
	return s.ResolveMutationError(ctx, conversationID, err)
}

func pendingExpiredBody(ttl time.Duration) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"message": "pending turn expired after " + ttl.String(),
			"type":    "request_timeout",
			"code":    "request_timeout",
		},
	}
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && raw != "" {
		return raw
	}
	return fallback
}

type pendingRegistryLike interface {
	WaitTurn(context.Context, *PendingTurn) (PendingResult, error)
	StartDelta(string) (string, error)
	RevertFinalize(string, string)
	Publish(string, PendingEvent) error
	StartComplete(string) (string, error)
	Resolve(string, PendingResult) error
	StartAbort(string) (string, error)
	Abort(string, map[string]any) error
	ExpireOlderThan(time.Time, map[string]any) int
	Add(*PendingTurn)
	GetByConversationID(string) (*PendingTurn, bool)
}
