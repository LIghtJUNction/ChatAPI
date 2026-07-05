package service

import (
	"context"
	"errors"
	"sync"
)

var ErrPendingNotFound = errors.New("pending turn not found")

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
	Events         chan PendingEvent
	done           chan PendingResult
}

type PendingRegistry struct {
	mu               sync.RWMutex
	byConversationID map[string]*PendingTurn
}

func NewPendingRegistry() *PendingRegistry {
	return &PendingRegistry{
		byConversationID: make(map[string]*PendingTurn),
	}
}

func (r *PendingRegistry) Add(turn *PendingTurn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byConversationID[turn.ConversationID] = turn
}

func (r *PendingRegistry) GetByConversationID(conversationID string) (*PendingTurn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	turn, ok := r.byConversationID[conversationID]
	return turn, ok
}

func (r *PendingRegistry) Resolve(conversationID string, result PendingResult) error {
	r.mu.Lock()
	turn, ok := r.byConversationID[conversationID]
	if ok {
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
	select {
	case turn.Events <- event:
	default:
	}
	return nil
}
