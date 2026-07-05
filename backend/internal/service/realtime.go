package service

import (
	"context"
	"sync"

	"github.com/zyf/chatapi/internal/store"
)

type Event struct {
	Type                   string               `json:"type"`
	Conversations          []store.Conversation `json:"conversations,omitempty"`
	Conversation           *store.Conversation  `json:"conversation,omitempty"`
	Messages               []store.Message      `json:"messages,omitempty"`
	ConversationID         string               `json:"conversation_id,omitempty"`
	CurrentConnectionCount int                  `json:"current_connection_count,omitempty"`
}

type Subscription struct {
	Events chan Event
}

type RealtimeHub struct {
	mu    sync.RWMutex
	subs  map[*Subscription]struct{}
	store store.Store
}

func NewRealtimeHub(dataStore store.Store) *RealtimeHub {
	return &RealtimeHub{
		subs:  make(map[*Subscription]struct{}),
		store: dataStore,
	}
}

func (h *RealtimeHub) Subscribe() *Subscription {
	sub := &Subscription{Events: make(chan Event, 16)}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	count := len(h.subs)
	h.mu.Unlock()
	h.broadcast(Event{Type: "connection_count", CurrentConnectionCount: count})
	return sub
}

func (h *RealtimeHub) Unsubscribe(sub *Subscription) {
	h.mu.Lock()
	if _, ok := h.subs[sub]; ok {
		delete(h.subs, sub)
		close(sub.Events)
	}
	count := len(h.subs)
	h.mu.Unlock()
	h.broadcast(Event{Type: "connection_count", CurrentConnectionCount: count})
}

func (h *RealtimeHub) Snapshot() (Event, error) {
	conversations, err := h.store.ListConversations(context.Background())
	if err != nil {
		return Event{}, err
	}
	return Event{
		Type:          "snapshot",
		Conversations: conversations,
	}, nil
}

func (h *RealtimeHub) PublishConversationUpsert(conversation store.Conversation, messages []store.Message) {
	h.broadcast(Event{
		Type:         "conversation_upsert",
		Conversation: &conversation,
		Messages:     messages,
	})
}

func (h *RealtimeHub) PublishConversationDelete(conversationID string) {
	h.broadcast(Event{
		Type:           "conversation_delete",
		ConversationID: conversationID,
	})
}

func (h *RealtimeHub) broadcast(event Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.subs {
		select {
		case sub.Events <- event:
		default:
		}
	}
}
