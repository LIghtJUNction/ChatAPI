package turn

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	conversationresolve "github.com/zyf2007/ChatAPI/internal/service/chat/conversationresolve"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
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
	Resolver                    *conversationresolve.Service
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
	if target, err := s.resolveTarget(ctx, input); err != nil {
		return nil, err
	} else {
		input.Target = target
	}
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
	go s.disconnectOnRequestDone(ctx, turn.ConversationID, turn.RequestID, "request disconnected")
	result, err := s.Pending.WaitTurn(ctx, turn)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			_ = s.disconnectPendingRequest(context.Background(), turn.ConversationID, turn.RequestID, "request disconnected", "request_disconnected", "Request Disconnected")
		}
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
	if target, err := s.resolveTarget(ctx, input); err != nil {
		return nil, common.Conversation{}, err
	} else {
		input.Target = target
	}
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
	go s.disconnectOnRequestDone(ctx, conversation.ID, turn.RequestID, "request disconnected")
	return turn, conversation, nil
}

func (s *Service) DisconnectRecoveredPending(ctx context.Context, reason string) (ExpireResult, error) {
	items, err := s.Store.ListConversations(ctx)
	if err != nil {
		return ExpireResult{}, err
	}
	result := ExpireResult{}
	for _, item := range items {
		status := strings.TrimSpace(stringValue(item.Metadata["realtime_status"], ""))
		if status != "waiting" && status != "streaming" {
			continue
		}
		requestID := s.latestConversationRequestID(ctx, item.ID)
		if err := s.disconnectPendingRequest(ctx, item.ID, requestID, reason, "recovered_pending_disconnected", "Recovered Pending Disconnected"); err == nil || errors.Is(err, common.ErrPendingDisconnected) {
			result.ExpiredConversations++
		} else {
			return ExpireResult{}, err
		}
	}
	return result, nil
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
	requestID := s.pendingRequestID(conversationID)
	conversation, _, err := s.Store.AbortPendingTurn(ctx, common.AbortPendingInput{
		ConversationID: conversationID,
		Reason:         reason,
		Event: &common.AppendConversationEventInput{
			OwnerID:   s.ownerID(ctx),
			Type:      "request_aborted",
			Level:     "warn",
			Title:     "Request Aborted",
			Detail:    reason,
			RequestID: requestID,
			Metadata:  map[string]any{"reason": reason},
		},
	})
	if err != nil {
		s.Pending.RevertFinalize(conversationID, previousState)
		logging.BindContext(s.Logger, ctx,
			zap.String("conversation.id", conversationID),
			zap.String("turn.action", "abort"),
		).Error("turn abort persistence failed", zap.Error(err))
		return err
	}
	s.publishConversationState(ctx, conversationID)
	s.publishLatestTimelineEvent(ctx, conversationID, conversation)
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
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}

func (s *Service) resolveTarget(ctx context.Context, input SubmitInput) (SubmitTarget, error) {
	if s.Resolver == nil {
		return SubmitTarget{}, nil
	}
	target, err := s.Resolver.Resolve(ctx, conversationresolve.ResolveInput{
		OwnerID: input.OwnerID,
		Request: input.Request,
		RawBody: input.RawBody,
	})
	if err != nil {
		return SubmitTarget{}, err
	}
	return SubmitTarget{
		ConversationID: target.ConversationID,
		Reuse:          target.Reuse,
		Source:         target.Source,
	}, nil
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
	FindByToolCallID(string, string) (*PendingTurn, bool)
	FindConversationIDByToolCallID(string, string) (string, bool)
}

func (s *Service) disconnectOnRequestDone(ctx context.Context, conversationID string, requestID string, reason string) {
	if s == nil {
		return
	}
	if ctx == nil {
		return
	}
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.Canceled) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return
	}
	if _, pending := s.Pending.GetByConversationID(conversationID); !pending {
		return
	}
	_ = s.disconnectPendingRequest(context.Background(), conversationID, requestID, reason, "request_disconnected", "Request Disconnected")
}

func (s *Service) disconnectPendingRequest(ctx context.Context, conversationID string, requestID string, reason string, eventType string, title string) error {
	conversation, _, err := s.Store.DisconnectPendingTurn(ctx, common.DisconnectPendingInput{
		ConversationID: conversationID,
		Reason:         reason,
		Event: &common.AppendConversationEventInput{
			OwnerID:   s.eventOwnerID(ctx, conversationID),
			Type:      eventType,
			Level:     "warn",
			Title:     title,
			Detail:    reason,
			RequestID: requestID,
			Metadata:  map[string]any{"reason": reason},
		},
	})
	if err != nil {
		if errors.Is(err, common.ErrPendingDisconnected) {
			return err
		}
		return err
	}
	s.publishConversationState(ctx, conversationID)
	s.publishLatestTimelineEvent(ctx, conversationID, conversation)
	_ = s.Pending.Abort(conversationID, map[string]any{
		"error": map[string]any{
			"message": reason,
			"type":    eventType,
			"code":    eventType,
		},
	})
	logging.BindContext(s.Logger, context.Background(),
		zap.String("conversation.id", conversationID),
		zap.String("request.id", requestID),
	).Info("pending request disconnected")
	return nil
}

func (s *Service) publishConversationState(ctx context.Context, conversationID string) {
	if s == nil || s.Submitter == nil || s.Submitter.Realtime == nil {
		return
	}
	conversation, err := s.Store.GetConversation(ctx, conversationID)
	if err != nil {
		return
	}
	messages, err := s.Store.ListMessages(ctx, conversationID)
	if err != nil {
		s.Submitter.Realtime.PublishConversationUpsert(conversation, nil)
		return
	}
	s.Submitter.Realtime.PublishConversationUpsert(conversation, messages)
}

func (s *Service) publishTimelineEvent(conversation common.Conversation, event common.ConversationEvent) {
	if s == nil || s.Submitter == nil || s.Submitter.Realtime == nil {
		return
	}
	ownerID := strings.TrimSpace(stringValue(conversation.Metadata["owner_id"], ""))
	if ownerID == "" {
		return
	}
	s.Submitter.Realtime.PublishTimelineItemAppend(ownerID, conversation, timelinesvc.Item{
		ID:        "evt:" + event.ID,
		Kind:      "system_event",
		CreatedAt: event.CreatedAt,
		Event:     &event,
	})
}

func (s *Service) publishLatestTimelineEvent(ctx context.Context, conversationID string, conversation common.Conversation) {
	if s == nil || s.Store == nil {
		return
	}
	items, err := s.Store.ListConversationEvents(ctx, conversationID)
	if err != nil || len(items) == 0 {
		return
	}
	s.publishTimelineEvent(conversation, items[len(items)-1])
}

func (s *Service) pendingRequestID(conversationID string) string {
	if s == nil || s.Pending == nil || strings.TrimSpace(conversationID) == "" {
		return ""
	}
	turn, ok := s.Pending.GetByConversationID(conversationID)
	if !ok || turn == nil {
		return ""
	}
	return strings.TrimSpace(turn.RequestID)
}

func (s *Service) latestConversationRequestID(ctx context.Context, conversationID string) string {
	if s == nil || s.Store == nil || strings.TrimSpace(conversationID) == "" {
		return ""
	}
	items, err := s.Store.ListMessages(ctx, conversationID)
	if err != nil {
		return ""
	}
	for i := len(items) - 1; i >= 0; i-- {
		requestDebug, _ := items[i].Metadata["request_debug"].(map[string]any)
		if requestID := strings.TrimSpace(stringValue(requestDebug["request_id"], "")); requestID != "" {
			return requestID
		}
	}
	return ""
}

func (s *Service) eventOwnerID(ctx context.Context, conversationID string) string {
	if ownerID := strings.TrimSpace(s.ownerID(ctx)); ownerID != "" {
		return ownerID
	}
	if s == nil || s.Pending == nil || strings.TrimSpace(conversationID) == "" {
		return ""
	}
	turn, ok := s.Pending.GetByConversationID(conversationID)
	if !ok || turn == nil {
		return ""
	}
	return strings.TrimSpace(turn.OwnerID)
}
