package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/zyf/chatapi/internal/store"
)

const realtimeSlowDisconnectThreshold = 3

var ErrRealtimeConnectionLimitExceeded = errors.New("realtime connection limit exceeded")

type RealtimeConnectionKind string

const (
	RealtimeConnectionWebUI RealtimeConnectionKind = "webui"
	RealtimeConnectionAPI   RealtimeConnectionKind = "api"
	RealtimeConnectionSSE   RealtimeConnectionKind = "sse"
)

type RealtimeLimits struct {
	MaxConnections        int
	MaxConnectionsPerUser int
	WebUIReservedPerUser  int
}

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
	lease      *RealtimeLease
	dropStreak int
}

type RealtimeHub struct {
	mu              sync.RWMutex
	subs            map[*Subscription]struct{}
	connections     map[*RealtimeLease]struct{}
	store           store.Store
	limits          RealtimeLimits
	recoverableDrop atomic.Int64
	criticalDrop    atomic.Int64
	slowDisconnect  atomic.Int64
	rejected        atomic.Int64
}

type RealtimeStats struct {
	Subscribers           int `json:"subscribers"`
	WebUISubscribers      int `json:"webui_subscribers"`
	APIConnections        int `json:"api_connections"`
	SSEConnections        int `json:"sse_connections"`
	TotalConnections      int `json:"total_connections"`
	QueuedEvents          int `json:"queued_events"`
	MaxQueueCapacity      int `json:"max_queue_capacity"`
	RecoverableDrops      int `json:"recoverable_drops"`
	CriticalDrops         int `json:"critical_drops"`
	SlowDisconnects       int `json:"slow_disconnects"`
	RejectedConnections   int `json:"rejected_connections"`
	MaxConnections        int `json:"max_connections"`
	MaxConnectionsPerUser int `json:"max_connections_per_user"`
	WebUIReservedPerUser  int `json:"webui_reserved_per_user"`
}

type RealtimeSubscribeOptions struct {
	OwnerID string
	Kind    RealtimeConnectionKind
}

type RealtimeLease struct {
	ownerID string
	kind    RealtimeConnectionKind
	hub     *RealtimeHub
	once    sync.Once
}

func NewRealtimeHub(dataStore store.Store, limits ...RealtimeLimits) *RealtimeHub {
	appliedLimits := RealtimeLimits{}
	if len(limits) > 0 {
		appliedLimits = limits[0]
	}
	return &RealtimeHub{
		subs:        make(map[*Subscription]struct{}),
		connections: make(map[*RealtimeLease]struct{}),
		store:       dataStore,
		limits:      appliedLimits,
	}
}

func NewRealtimeLimits(maxConnections int, maxConnectionsPerUser int, webUIReservedPerUser int) RealtimeLimits {
	return RealtimeLimits{
		MaxConnections:        maxConnections,
		MaxConnectionsPerUser: maxConnectionsPerUser,
		WebUIReservedPerUser:  webUIReservedPerUser,
	}
}

func (h *RealtimeHub) Subscribe(ctx context.Context, options RealtimeSubscribeOptions) (*Subscription, Event, error) {
	if options.Kind == "" {
		options.Kind = RealtimeConnectionWebUI
	}
	lease, err := h.Acquire(ctx, options)
	if err != nil {
		return nil, Event{}, err
	}

	snapshot, err := h.snapshot(ctx)
	if err != nil {
		lease.Release()
		return nil, Event{}, err
	}

	sub := &Subscription{Events: make(chan Event, 16), lease: lease}
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

	if sub.lease != nil {
		sub.lease.Release()
	}
	h.broadcast(Event{Type: "connection_count", CurrentConnectionCount: count})
}

func (h *RealtimeHub) Acquire(_ context.Context, options RealtimeSubscribeOptions) (*RealtimeLease, error) {
	if options.Kind == "" {
		options.Kind = RealtimeConnectionAPI
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.canAcquireLocked(options) {
		h.rejected.Add(1)
		return nil, ErrRealtimeConnectionLimitExceeded
	}
	lease := &RealtimeLease{
		ownerID: options.OwnerID,
		kind:    options.Kind,
		hub:     h,
	}
	h.connections[lease] = struct{}{}
	return lease, nil
}

func (l *RealtimeLease) Release() {
	if l == nil || l.hub == nil {
		return
	}
	l.once.Do(func() {
		l.hub.release(l)
	})
}

func (h *RealtimeHub) release(lease *RealtimeLease) {
	h.mu.Lock()
	delete(h.connections, lease)
	h.mu.Unlock()
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
		RecoverableDrops:      int(h.recoverableDrop.Load()),
		CriticalDrops:         int(h.criticalDrop.Load()),
		SlowDisconnects:       int(h.slowDisconnect.Load()),
		RejectedConnections:   int(h.rejected.Load()),
		MaxConnections:        h.limits.MaxConnections,
		MaxConnectionsPerUser: h.limits.MaxConnectionsPerUser,
		WebUIReservedPerUser:  h.limits.WebUIReservedPerUser,
	}
	for sub := range h.subs {
		stats.Subscribers++
		stats.QueuedEvents += len(sub.Events)
		stats.MaxQueueCapacity += cap(sub.Events)
	}
	for lease := range h.connections {
		stats.TotalConnections++
		switch lease.kind {
		case RealtimeConnectionWebUI:
			stats.WebUISubscribers++
		case RealtimeConnectionSSE:
			stats.SSEConnections++
		default:
			stats.APIConnections++
		}
	}
	return stats
}

func (h *RealtimeHub) canAcquireLocked(options RealtimeSubscribeOptions) bool {
	if h.limits.MaxConnections > 0 && len(h.connections) >= h.limits.MaxConnections {
		return false
	}
	if h.limits.MaxConnectionsPerUser <= 0 || options.OwnerID == "" {
		return true
	}
	ownerTotal, ownerWebUI := h.ownerCountsLocked(options.OwnerID)
	if ownerTotal >= h.limits.MaxConnectionsPerUser {
		return false
	}
	if options.Kind == RealtimeConnectionWebUI {
		return true
	}
	reservedMissing := h.limits.WebUIReservedPerUser - ownerWebUI
	if reservedMissing <= 0 {
		return true
	}
	usableByNonWebUI := h.limits.MaxConnectionsPerUser - reservedMissing
	if usableByNonWebUI < 0 {
		usableByNonWebUI = 0
	}
	return ownerTotal < usableByNonWebUI
}

func (h *RealtimeHub) ownerCountsLocked(ownerID string) (total int, webUI int) {
	for lease := range h.connections {
		if lease.ownerID != ownerID {
			continue
		}
		total++
		if lease.kind == RealtimeConnectionWebUI {
			webUI++
		}
	}
	return total, webUI
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
				if sub.lease != nil {
					delete(h.connections, sub.lease)
				}
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
