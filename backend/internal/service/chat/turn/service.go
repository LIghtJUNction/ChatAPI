package turn

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	conversationresolve "github.com/zyf2007/ChatAPI/internal/service/chat/conversationresolve"
	conversationstate "github.com/zyf2007/ChatAPI/internal/service/chat/conversationstate"
	egresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/egress"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
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

type TurnIdentity struct {
	OwnerID   string
	RequestID string
}

type eventRoute struct {
	OwnerID        string
	RequestID      string
	ConversationID string
}

func eventRouteFromTurn(turn *PendingTurn) eventRoute {
	if turn == nil {
		return eventRoute{}
	}
	return eventRoute{
		OwnerID:        strings.TrimSpace(turn.OwnerID),
		RequestID:      strings.TrimSpace(turn.RequestID),
		ConversationID: strings.TrimSpace(turn.ConversationID),
	}
}

func eventRouteFromIdentity(conversationID string, identity TurnIdentity) eventRoute {
	return eventRoute{
		OwnerID:        strings.TrimSpace(identity.OwnerID),
		RequestID:      strings.TrimSpace(identity.RequestID),
		ConversationID: strings.TrimSpace(conversationID),
	}
}

func (r eventRoute) withConversation(conversation common.Conversation) eventRoute {
	if r.OwnerID == "" {
		r.OwnerID = conversationstate.OwnerID(conversation)
	}
	if r.ConversationID == "" {
		r.ConversationID = strings.TrimSpace(conversation.ID)
	}
	return r
}

type SubmitPrincipal struct {
	OwnerID string
	Actor   actor.Actor
}

type Service struct {
	Submitter                   *Submitter
	Pending                     pendingRegistryLike
	Store                       chat.Store
	Resolver                    *conversationresolve.Service
	Events                      chatevents.Publisher
	ResolveMutationError        MutationErrorResolver
	NotifyText                  TextNotifier
	EnsureMessageAdmission      AdmissionHook
	EnsureConversationAdmission AdmissionHook
	OwnerIDFromContext          func(context.Context) string
	ActorFromContext            func(context.Context) (actor.Actor, bool)
	Egress                      *egresssvc.Service
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
	principal := s.submitPrincipal(ctx)
	if err := s.ensureAdmissions(ctx, principal.OwnerID); err != nil {
		return nil, err
	}
	input.OwnerID = principal.OwnerID
	input.Actor = principal.Actor
	if target, err := s.resolveTarget(ctx, input); err != nil {
		return nil, err
	} else {
		input.Target = target
	}
	turn, conversation, message, err := s.Submitter.Submit(ctx, input)
	if err != nil {
		logging.BindContext(s.Logger, ctx, zap.String("owner.id", principal.OwnerID)).Error("create pending response submit failed", zap.Error(err))
		return nil, err
	}
	route := eventRouteFromTurn(turn)
	s.publishConversationUpserted(ctx, route, conversation)
	s.publishMessageAppended(ctx, route, conversation, message)
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", principal.OwnerID),
		zap.String("conversation.id", turn.ConversationID),
		zap.String("request.id", turn.RequestID),
	).Info("pending response created")
	go s.disconnectOnRequestDone(ctx, turn.ConversationID, turn.RequestID, "request disconnected")
	result, err := s.Pending.WaitTurn(ctx, turn)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			_ = s.disconnectPendingRequest(context.Background(), turn.ConversationID, TurnIdentity{RequestID: turn.RequestID, OwnerID: turn.OwnerID}, "request disconnected", "request_disconnected", "Request Disconnected")
		}
		logging.BindContext(s.Logger, ctx,
			zap.String("owner.id", principal.OwnerID),
			zap.String("conversation.id", turn.ConversationID),
			zap.String("request.id", turn.RequestID),
		).Warn("pending response wait interrupted", zap.Error(err))
		return nil, err
	}
	return result.ResponseBody, nil
}

func (s *Service) CreatePendingStream(ctx context.Context, input SubmitInput) (*PendingTurn, common.Conversation, error) {
	principal := s.submitPrincipal(ctx)
	if err := s.ensureAdmissions(ctx, principal.OwnerID); err != nil {
		return nil, common.Conversation{}, err
	}
	input.OwnerID = principal.OwnerID
	input.Actor = principal.Actor
	if target, err := s.resolveTarget(ctx, input); err != nil {
		return nil, common.Conversation{}, err
	} else {
		input.Target = target
	}
	turn, conversation, message, err := s.Submitter.Submit(ctx, input)
	if err != nil {
		logging.BindContext(s.Logger, ctx, zap.String("owner.id", principal.OwnerID)).Error("create pending stream submit failed", zap.Error(err))
		return nil, common.Conversation{}, err
	}
	route := eventRouteFromTurn(turn)
	s.publishConversationUpserted(ctx, route, conversation)
	s.publishMessageAppended(ctx, route, conversation, message)
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", principal.OwnerID),
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
		if !conversationstate.IsPendingStatus(conversationstate.FromConversation(item).Status) {
			continue
		}
		identity, resolveErr := s.resolveStoredTurnIdentity(ctx, item.ID)
		if resolveErr != nil && !errors.Is(resolveErr, common.ErrNotFound) {
			return ExpireResult{}, resolveErr
		}
		if err := s.disconnectPendingRequest(ctx, item.ID, identity, reason, "recovered_pending_disconnected", "Recovered Pending Disconnected"); err == nil || errors.Is(err, common.ErrPendingDisconnected) {
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
	existing := conversationstate.FromConversation(conversation).DraftText
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
	s.publishConversationUpserted(ctx, s.eventRouteForContext(ctx, updated), updated)
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
	route := s.eventRouteForContext(ctx, conversation)
	s.publishConversationUpserted(ctx, route, conversation)
	responseBody := s.egress().CompleteBody(conversation, input, message)
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
	s.publishMessageAppended(ctx, route, conversation, message)
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
	identity, err := s.resolveActiveTurnIdentity(ctx, conversationID)
	if err != nil {
		s.Pending.RevertFinalize(conversationID, previousState)
		return err
	}
	result, err := s.Store.AbortPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: conversationID,
		Reason:         reason,
		Identity:       common.TurnIdentity{OwnerID: identity.OwnerID, RequestID: identity.RequestID},
		EventType:      "request_aborted",
		EventLevel:     "warn",
		EventTitle:     "Request Aborted",
		EventDetail:    reason,
		EventMetadata:  map[string]any{"reason": reason},
	})
	if err != nil {
		s.Pending.RevertFinalize(conversationID, previousState)
		logging.BindContext(s.Logger, ctx,
			zap.String("conversation.id", conversationID),
			zap.String("turn.action", "abort"),
		).Error("turn abort persistence failed", zap.Error(err))
		return err
	}
	conversation := result.Conversation
	route := eventRouteFromIdentity(conversationID, identity)
	s.publishConversationUpserted(ctx, route, conversation)
	s.publishConversationEventAppended(ctx, route, conversation, result.Event)
	body := s.egress().AbortBody(conversation, reason)
	_ = s.Pending.Publish(conversationID, PendingEvent{Type: "abort", ErrorBody: body})
	s.notifyText(ctx, identity.OwnerID, conversation.Title, reason)
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", identity.OwnerID),
		zap.String("conversation.id", conversationID),
		zap.String("request.format", conversationstate.RequestFormat(conversation)),
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

func (s *Service) submitPrincipal(ctx context.Context) SubmitPrincipal {
	return SubmitPrincipal{
		OwnerID: strings.TrimSpace(s.ownerID(ctx)),
		Actor:   s.actor(ctx),
	}
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

func (s *Service) egress() *egresssvc.Service {
	if s != nil && s.Egress != nil {
		return s.Egress
	}
	return egresssvc.New()
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
	_ = s.disconnectPendingRequest(context.Background(), conversationID, TurnIdentity{RequestID: requestID}, reason, "request_disconnected", "Request Disconnected")
}

func (s *Service) disconnectPendingRequest(ctx context.Context, conversationID string, identity TurnIdentity, reason string, eventType string, title string) error {
	resolved, err := s.resolveIdentityForMutation(ctx, conversationID, identity)
	if err != nil {
		return err
	}
	result, err := s.Store.DisconnectPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: conversationID,
		Reason:         reason,
		Identity:       common.TurnIdentity{OwnerID: resolved.OwnerID, RequestID: resolved.RequestID},
		EventType:      eventType,
		EventLevel:     "warn",
		EventTitle:     title,
		EventDetail:    reason,
		EventMetadata:  map[string]any{"reason": reason},
	})
	if err != nil {
		if errors.Is(err, common.ErrPendingDisconnected) {
			return err
		}
		return err
	}
	conversation := result.Conversation
	route := eventRouteFromIdentity(conversationID, resolved)
	s.publishConversationUpserted(ctx, route, conversation)
	s.publishConversationEventAppended(ctx, route, conversation, result.Event)
	_ = s.Pending.Abort(conversationID, map[string]any{
		"error": map[string]any{
			"message": reason,
			"type":    eventType,
			"code":    eventType,
		},
	})
	logging.BindContext(s.Logger, context.Background(),
		zap.String("conversation.id", conversationID),
		zap.String("request.id", resolved.RequestID),
	).Info("pending request disconnected")
	return nil
}

func (s *Service) publishConversationUpserted(ctx context.Context, route eventRoute, conversation common.Conversation) {
	if s == nil || s.Events == nil {
		return
	}
	route = route.withConversation(conversation)
	if route.OwnerID == "" {
		return
	}
	s.Events.Publish(ctx, chatevents.Event{
		Type:           chatevents.TypeConversationUpserted,
		OwnerID:        route.OwnerID,
		ConversationID: route.ConversationID,
		Conversation:   conversation,
	})
}

func (s *Service) publishMessageAppended(ctx context.Context, route eventRoute, conversation common.Conversation, message common.Message) {
	if s == nil || s.Events == nil {
		return
	}
	route = route.withConversation(conversation)
	if route.OwnerID == "" {
		return
	}
	msg := message
	s.Events.Publish(ctx, chatevents.Event{
		Type:           chatevents.TypeMessageAppended,
		OwnerID:        route.OwnerID,
		ConversationID: route.ConversationID,
		Conversation:   conversation,
		Message:        &msg,
	})
}

func (s *Service) publishConversationEventAppended(ctx context.Context, route eventRoute, conversation common.Conversation, event common.ConversationEvent) {
	if s == nil || s.Events == nil {
		return
	}
	route = route.withConversation(conversation)
	if route.OwnerID == "" {
		return
	}
	evt := event
	s.Events.Publish(ctx, chatevents.Event{
		Type:              chatevents.TypeConversationEventAppended,
		OwnerID:           route.OwnerID,
		ConversationID:    route.ConversationID,
		Conversation:      conversation,
		ConversationEvent: &evt,
	})
}

func (s *Service) eventRouteForContext(ctx context.Context, conversation common.Conversation) eventRoute {
	return eventRoute{
		OwnerID:        strings.TrimSpace(s.ownerID(ctx)),
		ConversationID: strings.TrimSpace(conversation.ID),
	}.withConversation(conversation)
}

func (s *Service) resolveActiveTurnIdentity(ctx context.Context, conversationID string) (TurnIdentity, error) {
	if s == nil {
		return TurnIdentity{}, nil
	}
	if turn, ok := s.Pending.GetByConversationID(conversationID); ok && turn != nil {
		return TurnIdentity{
			OwnerID:   strings.TrimSpace(turn.OwnerID),
			RequestID: strings.TrimSpace(turn.RequestID),
		}, nil
	}
	return s.resolveStoredTurnIdentity(ctx, conversationID)
}

func (s *Service) resolveStoredTurnIdentity(ctx context.Context, conversationID string) (TurnIdentity, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(conversationID) == "" {
		return TurnIdentity{}, common.ErrNotFound
	}
	req, err := s.Store.GetLatestRequestForConversation(ctx, conversationID)
	if err != nil {
		return TurnIdentity{}, err
	}
	return TurnIdentity{
		OwnerID:   strings.TrimSpace(req.OwnerID),
		RequestID: strings.TrimSpace(req.RequestID),
	}, nil
}

func (s *Service) resolveIdentityForMutation(ctx context.Context, conversationID string, base TurnIdentity) (TurnIdentity, error) {
	identity := TurnIdentity{
		OwnerID:   strings.TrimSpace(base.OwnerID),
		RequestID: strings.TrimSpace(base.RequestID),
	}
	if identity.OwnerID != "" && identity.RequestID != "" {
		return identity, nil
	}
	resolved, err := s.resolveActiveTurnIdentity(ctx, conversationID)
	if err != nil {
		if identity.OwnerID != "" || identity.RequestID != "" {
			return identity, nil
		}
		return TurnIdentity{}, err
	}
	if identity.OwnerID == "" {
		identity.OwnerID = resolved.OwnerID
	}
	if identity.RequestID == "" {
		identity.RequestID = resolved.RequestID
	}
	if identity.OwnerID == "" {
		identity.OwnerID = strings.TrimSpace(s.ownerID(ctx))
	}
	return identity, nil
}
