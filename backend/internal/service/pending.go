package service

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrPendingNotFound = errors.New("pending turn not found")
var ErrPendingConflict = errors.New("pending turn already finalized")

type PendingResult struct {
	ResponseBody map[string]any
}

type PendingEvent struct {
	Type         string
	DeltaText    string
	OutputText   string
	Mode         string
	ToolName     string
	ToolCallID   string
	ToolOutput   string
	ResponseBody map[string]any
	ErrorBody    map[string]any
}

type PendingTurn struct {
	RequestID      string
	ConversationID string
	ResponseID     string
	RequestFormat  string
	Model          string
	CreatedAt      time.Time
	State          string
	Events         chan PendingEvent
	done           chan PendingResult
}

type PendingRegistry struct {
	mu               sync.RWMutex
	byConversationID map[string]*PendingTurn
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
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = time.Now().UTC()
	}
	if turn.State == "" {
		turn.State = "pending"
	}
	r.byConversationID[turn.ConversationID] = turn
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
	close(turn.Events)
	turn.done <- result
	close(turn.done)
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
		_ = publishPendingEvent(turn, PendingEvent{
			Type:      "abort",
			ErrorBody: body,
		})
		close(turn.Events)
		turn.done <- PendingResult{ResponseBody: body}
		close(turn.done)
	}
	return len(expired)
}

func (r *PendingRegistry) Wait(ctx context.Context, conversationID string) (PendingResult, error) {
	turn, ok := r.GetByConversationID(conversationID)
	if !ok {
		return PendingResult{}, ErrPendingNotFound
	}
	select {
	case <-ctx.Done():
		return PendingResult{}, ctx.Err()
	case result := <-turn.done:
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
		return previousState, nil
	default:
		return "", ErrPendingConflict
	}
}
