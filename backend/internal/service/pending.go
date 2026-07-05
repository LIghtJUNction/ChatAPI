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

type PendingTurn struct {
	RequestID      string
	ConversationID string
	ResponseID     string
	RequestFormat  string
	Model          string
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
