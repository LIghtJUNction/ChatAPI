package service

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/zyf/chatapi/internal/store"
)

const realtimeSlowDisconnectThreshold = 3

type Event struct {
	Type                   string               `json:"type"`
	Conversations          []store.Conversation `json:"conversations,omitempty"`
	Conversation           *store.Conversation  `json:"conversation,omitempty"`
	Messages               []store.Message      `json:"messages,omitempty"`
	ConversationID         string               `json:"conversation_id,omitempty"`
	CurrentConnectionCount int                  `json:"current_connection_count,omitempty"`
}

type Subscription struct {
	Events     chan Event
	dropStreak int
}

type RealtimeHub struct {
	mu              sync.RWMutex
	subs            map[*Subscription]struct{}
	store           store.Store
	recoverableDrop atomic.Int64
	criticalDrop    atomic.Int64
	slowDisconnect  atomic.Int64
}

type RealtimeStats struct {
	Subscribers      int `json:"subscribers"`
	QueuedEvents     int `json:"queued_events"`
	MaxQueueCapacity int `json:"max_queue_capacity"`
	RecoverableDrops int `json:"recoverable_drops"`
	CriticalDrops    int `json:"critical_drops"`
	SlowDisconnects  int `json:"slow_disconnects"`
}

func NewRealtimeHub(dataStore store.Store) *RealtimeHub {
	return &RealtimeHub{
		subs:  make(map[*Subscription]struct{}),
		store: dataStore,
	}
}

func (h *RealtimeHub) Subscribe(ctx context.Context) (*Subscription, Event, error) {
	sub := &Subscription{Events: make(chan Event, 16)}

	snapshot, err := h.snapshot(ctx)
	if err != nil {
		return nil, Event{}, err
	}

	h.mu.Lock()
	h.subs[sub] = struct{}{}
	count := len(h.subs)
	h.mu.Unlock()

	h.broadcast(Event{Type: "connection_count", CurrentConnectionCount: count})
	return sub, snapshot, nil
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

func (h *RealtimeHub) snapshot(ctx context.Context) (Event, error) {
	conversations, err := h.store.ListConversations(ctx)
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

func (h *RealtimeHub) Stats() RealtimeStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	stats := RealtimeStats{
		RecoverableDrops: int(h.recoverableDrop.Load()),
		CriticalDrops:    int(h.criticalDrop.Load()),
		SlowDisconnects:  int(h.slowDisconnect.Load()),
	}
	for sub := range h.subs {
		stats.Subscribers++
		stats.QueuedEvents += len(sub.Events)
		stats.MaxQueueCapacity += cap(sub.Events)
	}
	return stats
}

func (h *RealtimeHub) broadcast(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	countChanged := false
	for sub := range h.subs {
		select {
		case sub.Events <- event:
			sub.dropStreak = 0
		default:
			h.recoverableDrop.Add(1)
			sub.dropStreak++
			if sub.dropStreak >= realtimeSlowDisconnectThreshold {
				delete(h.subs, sub)
				close(sub.Events)
				h.slowDisconnect.Add(1)
				countChanged = true
			}
		}
	}
	if !countChanged || event.Type == "connection_count" {
		return
	}
	count := len(h.subs)
	for sub := range h.subs {
		select {
		case sub.Events <- Event{Type: "connection_count", CurrentConnectionCount: count}:
			sub.dropStreak = 0
		default:
			h.criticalDrop.Add(1)
		}
	}
}
