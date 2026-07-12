package workspace

import "strings"

type PresenceEvent struct {
	UserID           string `json:"user_id"`
	ConnectionCount  int    `json:"connection_count"`
	TotalConnections int    `json:"total_connections"`
	Sequence         uint64 `json:"sequence"`
}

type PresenceSnapshot struct {
	UserConnections  map[string]int `json:"user_connections"`
	TotalConnections int            `json:"total_connections"`
	Sequence         uint64         `json:"sequence"`
}

type presenceSubscription struct {
	events  chan PresenceEvent
	userIDs map[string]struct{}
}

func (h *Hub) PresenceSnapshot(userIDs []string) PresenceSnapshot {
	if h == nil {
		return PresenceSnapshot{UserConnections: map[string]int{}}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	users := make(map[string]int, len(userIDs))
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID != "" {
			users[userID] = len(h.connections[userID])
		}
	}
	return PresenceSnapshot{UserConnections: users, TotalConnections: h.totalConnectionsLocked(), Sequence: h.presenceSeq}
}

func (h *Hub) SubscribePresence(userIDs []string) (<-chan PresenceEvent, func()) {
	subscription := &presenceSubscription{events: make(chan PresenceEvent, 16), userIDs: map[string]struct{}{}}
	for _, userID := range userIDs {
		if userID = strings.TrimSpace(userID); userID != "" {
			subscription.userIDs[userID] = struct{}{}
		}
	}
	h.presenceMu.Lock()
	h.presence[subscription] = struct{}{}
	h.presenceMu.Unlock()
	return subscription.events, func() {
		h.presenceMu.Lock()
		if _, ok := h.presence[subscription]; ok {
			delete(h.presence, subscription)
			close(subscription.events)
		}
		h.presenceMu.Unlock()
	}
}

func (h *Hub) totalConnectionsLocked() int {
	total := 0
	for _, connections := range h.connections {
		total += len(connections)
	}
	return total
}

func (h *Hub) nextPresenceSequenceLocked() uint64 {
	h.presenceSeq++
	return h.presenceSeq
}

func (h *Hub) publishPresence(userID string, count int, total int, sequence uint64) {
	event := PresenceEvent{UserID: strings.TrimSpace(userID), ConnectionCount: count, TotalConnections: total, Sequence: sequence}
	h.presenceMu.RLock()
	defer h.presenceMu.RUnlock()
	for subscriber := range h.presence {
		if _, ok := subscriber.userIDs[event.UserID]; !ok {
			continue
		}
		select {
		case subscriber.events <- event:
		default:
		}
	}
}
