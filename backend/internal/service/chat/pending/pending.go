package pending

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/zyf/chatapi/internal/protocol"
	"github.com/zyf/chatapi/internal/repository/common"
	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
	"go.uber.org/zap"
)

var ErrPendingNotFound = errors.New("pending turn not found")
var ErrPendingConflict = errors.New("pending turn already finalized")

type PendingResult = turnsvc.PendingResult
type PendingEvent = turnsvc.PendingEvent
type PendingTurn = turnsvc.PendingTurn

type PendingRegistry struct {
	mu               sync.RWMutex
	byConversationID map[string]*PendingTurn
	Logger           *zap.Logger
}

type PendingStats struct {
	Active   int            `json:"active"`
	ByState  map[string]int `json:"by_state"`
	ByModel  map[string]int `json:"by_model"`
	ByFormat map[string]int `json:"by_format"`
}

func NewPendingRegistry() *PendingRegistry {
	return &PendingRegistry{
		byConversationID: make(map[string]*PendingTurn),
	}
}

func (r *PendingRegistry) Add(turn *PendingTurn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if turn == nil {
		return
	}
	stored := *turn
	stored.NormalizedRequest = cloneTurnRequest(turn.NormalizedRequest)
	stored.RequestMeta = cloneRequestMeta(turn.RequestMeta)
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC()
	}
	if stored.State == "" {
		stored.State = "pending"
	}
	r.byConversationID[stored.ConversationID] = &stored
	r.loggerForTurn(&stored).Debug("pending turn registered")
}

func (r *PendingRegistry) GetByConversationID(conversationID string) (*PendingTurn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	turn, ok := r.byConversationID[conversationID]
	return turn, ok
}

func (r *PendingRegistry) Stats() PendingStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stats := PendingStats{
		Active:   len(r.byConversationID),
		ByState:  map[string]int{},
		ByModel:  map[string]int{},
		ByFormat: map[string]int{},
	}
	for _, turn := range r.byConversationID {
		state := turn.State
		if state == "" {
			state = "pending"
		}
		stats.ByState[state]++
		model := turn.Model
		if model == "" {
			model = "unknown"
		}
		stats.ByModel[model]++
		format := turn.RequestFormat
		if format == "" {
			format = "unknown"
		}
		stats.ByFormat[format]++
	}
	return stats
}

func (r *PendingRegistry) Resolve(conversationID string, result PendingResult) error {
	r.mu.Lock()
	turn, ok := r.byConversationID[conversationID]
	if ok {
		switch turn.State {
		case "aborting":
			turn.State = "aborted"
		default:
			turn.State = "completed"
		}
		delete(r.byConversationID, conversationID)
	}
	r.mu.Unlock()
	if !ok {
		return ErrPendingNotFound
	}
	r.loggerForTurn(turn).Debug("pending turn resolved")
	close(turn.Events)
	turn.Done <- result
	close(turn.Done)
	return nil
}

func (r *PendingRegistry) Abort(conversationID string, body map[string]any) error {
	return r.Resolve(conversationID, PendingResult{ResponseBody: body})
}

func (r *PendingRegistry) ExpireOlderThan(cutoff time.Time, body map[string]any) int {
	r.mu.Lock()
	expired := make([]*PendingTurn, 0)
	for conversationID, turn := range r.byConversationID {
		if turn.CreatedAt.IsZero() || !turn.CreatedAt.Before(cutoff) {
			continue
		}
		switch turn.State {
		case "pending", "streaming":
			turn.State = "expired"
			delete(r.byConversationID, conversationID)
			expired = append(expired, turn)
		}
	}
	r.mu.Unlock()

	for _, turn := range expired {
		r.loggerForTurn(turn).Warn("pending turn expired")
		_ = publishPendingEvent(turn, PendingEvent{
			Type:      "abort",
			ErrorBody: body,
		})
		close(turn.Events)
		turn.Done <- PendingResult{ResponseBody: body}
		close(turn.Done)
	}
	return len(expired)
}

func (r *PendingRegistry) Wait(ctx context.Context, conversationID string) (PendingResult, error) {
	turn, ok := r.GetByConversationID(conversationID)
	if !ok {
		return PendingResult{}, ErrPendingNotFound
	}
	return r.WaitTurn(ctx, turn)
}

func (r *PendingRegistry) WaitTurn(ctx context.Context, turn *PendingTurn) (PendingResult, error) {
	if turn == nil {
		return PendingResult{}, ErrPendingNotFound
	}
	select {
	case <-ctx.Done():
		r.loggerForTurn(turn).Warn("pending wait canceled", zap.Error(ctx.Err()))
		return PendingResult{}, ctx.Err()
	case result := <-turn.Done:
		r.loggerForTurn(turn).Debug("pending wait completed")
		return result, nil
	}
}

func (r *PendingRegistry) Publish(conversationID string, event PendingEvent) error {
	r.mu.RLock()
	turn, ok := r.byConversationID[conversationID]
	r.mu.RUnlock()
	if !ok {
		return ErrPendingNotFound
	}
	r.loggerForTurn(turn).Debug("pending event published", zap.String("event.type", event.Type))
	return publishPendingEvent(turn, event)
}

func (r *PendingRegistry) StartDelta(conversationID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	turn, ok := r.byConversationID[conversationID]
	if !ok {
		return "", ErrPendingNotFound
	}
	switch turn.State {
	case "pending", "streaming":
		previousState := turn.State
		turn.State = "streaming"
		return previousState, nil
	default:
		return "", ErrPendingConflict
	}
}

func publishPendingEvent(turn *PendingTurn, event PendingEvent) error {
	select {
	case turn.Events <- event:
	default:
	}
	return nil
}

func (r *PendingRegistry) StartComplete(conversationID string) (string, error) {
	return r.startFinalize(conversationID, "completing")
}

func (r *PendingRegistry) StartAbort(conversationID string) (string, error) {
	return r.startFinalize(conversationID, "aborting")
}

func (r *PendingRegistry) RevertFinalize(conversationID string, previousState string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	turn, ok := r.byConversationID[conversationID]
	if !ok {
		return
	}
	turn.State = previousState
}

func (r *PendingRegistry) startFinalize(conversationID string, nextState string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	turn, ok := r.byConversationID[conversationID]
	if !ok {
		return "", ErrPendingNotFound
	}
	switch turn.State {
	case "pending", "streaming":
		previousState := turn.State
		turn.State = nextState
		r.loggerForTurn(turn).Debug("pending turn state changed", zap.String("previous.state", previousState), zap.String("next.state", nextState))
		return previousState, nil
	default:
		return "", ErrPendingConflict
	}
}

func (r *PendingRegistry) loggerForTurn(turn *PendingTurn) *zap.Logger {
	logger := r.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	if turn == nil {
		return logger
	}
	fields := []zap.Field{
		zap.String("conversation.id", turn.ConversationID),
		zap.String("request.id", turn.RequestID),
		zap.String("response.id", turn.ResponseID),
		zap.String("owner.id", turn.OwnerID),
		zap.String("turn.state", turn.State),
		zap.String("model", turn.Model),
		zap.String("protocol", turn.RequestFormat),
	}
	if turn.Actor.UserID != "" {
		fields = append(fields,
			zap.String("actor.user_id", turn.Actor.UserID),
			zap.String("actor.role", turn.Actor.Role),
			zap.String("actor.source", turn.Actor.Source),
		)
	}
	return logger.With(fields...)
}

func cloneTurnRequest(input protocol.TurnRequest) protocol.TurnRequest {
	cloned := input
	if len(input.InputParts) > 0 {
		cloned.InputParts = append([]protocol.InputPart(nil), input.InputParts...)
	}
	if len(input.ToolSchemas) > 0 {
		cloned.ToolSchemas = make([]protocol.ToolSchema, 0, len(input.ToolSchemas))
		for _, item := range input.ToolSchemas {
			cloned.ToolSchemas = append(cloned.ToolSchemas, protocol.ToolSchema{
				Type:        item.Type,
				Name:        item.Name,
				Description: item.Description,
				Parameters:  pendingCloneAnyMap(item.Parameters),
				Raw:         pendingCloneAnyMap(item.Raw),
			})
		}
	}
	cloned.ResponseFormat = protocol.ResponseFormat{
		Type:   input.ResponseFormat.Type,
		Name:   input.ResponseFormat.Name,
		Schema: pendingCloneAnyMap(input.ResponseFormat.Schema),
	}
	return cloned
}

func cloneRequestMeta(input common.Request) common.Request {
	cloned := input
	cloned.RequestQuery = pendingCloneStringSliceMap(input.RequestQuery)
	cloned.RequestHeaders = pendingCloneStringSliceMap(input.RequestHeaders)
	cloned.RequestBody = pendingCloneAnyMap(input.RequestBody)
	if len(input.InputParts) > 0 {
		cloned.InputParts = append([]common.RequestInputPart(nil), input.InputParts...)
	}
	if len(input.ToolSchemas) > 0 {
		cloned.ToolSchemas = pendingCloneAnySlice(input.ToolSchemas)
	}
	cloned.ToolChoice = common.RequestToolChoice{
		Type: input.ToolChoice.Type,
		Name: input.ToolChoice.Name,
	}
	cloned.ResponseFormat = common.RequestResponseFormat{
		Type:   input.ResponseFormat.Type,
		Name:   input.ResponseFormat.Name,
		Schema: pendingCloneAnyMap(input.ResponseFormat.Schema),
	}
	cloned.Metadata = pendingCloneAnyMap(input.Metadata)
	return cloned
}

func pendingCloneStringSliceMap(input map[string][]string) map[string][]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(input))
	for key, values := range input {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func pendingCloneAnySlice(input []any) []any {
	if len(input) == 0 {
		return nil
	}
	cloned := make([]any, 0, len(input))
	for _, item := range input {
		cloned = append(cloned, pendingCloneAnyValue(item))
	}
	return cloned
}

func pendingCloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = pendingCloneAnyValue(value)
	}
	return cloned
}

func pendingCloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return pendingCloneAnyMap(typed)
	case []any:
		return pendingCloneAnySlice(typed)
	case []string:
		return append([]string(nil), typed...)
	case map[string][]string:
		return pendingCloneStringSliceMap(typed)
	default:
		return typed
	}
}
