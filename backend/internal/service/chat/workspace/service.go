package workspace

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
)

type ConversationQuery interface {
	ListConversationsForOwner(context.Context, string) ([]common.Conversation, error)
	ListMessagesForOwner(context.Context, string, string) ([]common.Message, error)
}

type PendingQuery interface {
	ListByOwnerID(string) []*PendingTurnView
}

type PendingTurnView struct {
	ConversationID string
}

type Snapshot struct {
	Type          string                `json:"type"`
	Conversations []common.Conversation `json:"conversations"`
}

type ConversationUpsert struct {
	Type         string              `json:"type"`
	Conversation common.Conversation `json:"conversation"`
	Messages     []common.Message    `json:"messages,omitempty"`
}

type ConversationDelete struct {
	Type           string `json:"type"`
	ConversationID string `json:"conversation_id"`
}

type TimelineItemAppend struct {
	Type           string           `json:"type"`
	ConversationID string           `json:"conversation_id"`
	Item           timelinesvc.Item `json:"item"`
	Conversation   common.Conversation `json:"conversation"`
}

type ConnectionCount struct {
	Type                   string `json:"type"`
	CurrentConnectionCount int    `json:"current_connection_count"`
}

type RealtimePublisher struct {
	hub *Hub
}

func NewRealtimePublisher(hub *Hub) *RealtimePublisher {
	return &RealtimePublisher{hub: hub}
}

func (p *RealtimePublisher) PublishConversationUpsert(conversation common.Conversation, messages []common.Message) {
	if p == nil || p.hub == nil {
		return
	}
	ownerID := ownerIDOfConversation(conversation)
	if ownerID == "" {
		return
	}
	p.hub.PublishConversationUpsert(ownerID, conversation, messages)
}

func (p *RealtimePublisher) PublishConversationDelete(ownerID string, conversationID string) {
	if p == nil || p.hub == nil {
		return
	}
	p.hub.PublishConversationDelete(strings.TrimSpace(ownerID), strings.TrimSpace(conversationID))
}

func (p *RealtimePublisher) PublishTimelineItemAppend(ownerID string, conversation common.Conversation, item timelinesvc.Item) {
	if p == nil || p.hub == nil {
		return
	}
	p.hub.PublishTimelineItemAppend(strings.TrimSpace(ownerID), conversation, item)
}

type Service struct {
	query ConversationQuery
}

func New(query ConversationQuery) *Service {
	return &Service{query: query}
}

func (s *Service) Snapshot(ctx context.Context, ownerID string) (Snapshot, error) {
	items, err := s.query.ListConversationsForOwner(ctx, strings.TrimSpace(ownerID))
	if err != nil {
		return Snapshot{}, err
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return Snapshot{Type: "snapshot", Conversations: items}, nil
}

type Hub struct {
	workspace *Service

	mu          sync.RWMutex
	connections map[string]map[*Connection]struct{}
}

func NewHub(workspace *Service) *Hub {
	return &Hub{
		workspace:   workspace,
		connections: map[string]map[*Connection]struct{}{},
	}
}

func (h *Hub) Register(ownerID string, conn *Connection) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.connections[ownerID]; !ok {
		h.connections[ownerID] = map[*Connection]struct{}{}
	}
	h.connections[ownerID][conn] = struct{}{}
	return len(h.connections[ownerID])
}

func (h *Hub) Unregister(ownerID string, conn *Connection) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if items, ok := h.connections[ownerID]; ok {
		delete(items, conn)
		if len(items) == 0 {
			delete(h.connections, ownerID)
			return 0
		}
		return len(items)
	}
	return 0
}

func (h *Hub) Snapshot(ctx context.Context, ownerID string) (Snapshot, error) {
	return h.workspace.Snapshot(ctx, ownerID)
}

func (h *Hub) ConnectionCount(ownerID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections[ownerID])
}

func (h *Hub) PublishConversationUpsert(ownerID string, conversation common.Conversation, messages []common.Message) {
	h.broadcast(ownerID, ConversationUpsert{
		Type:         "conversation_upsert",
		Conversation: conversation,
		Messages:     messages,
	})
}

func (h *Hub) PublishConversationDelete(ownerID string, conversationID string) {
	h.broadcast(ownerID, ConversationDelete{
		Type:           "conversation_delete",
		ConversationID: conversationID,
	})
}

func (h *Hub) PublishTimelineItemAppend(ownerID string, conversation common.Conversation, item timelinesvc.Item) {
	h.broadcast(ownerID, TimelineItemAppend{
		Type:           "timeline_item_append",
		ConversationID: conversation.ID,
		Item:           item,
		Conversation:   conversation,
	})
}

func (h *Hub) PublishConnectionCount(ownerID string) {
	h.broadcast(ownerID, ConnectionCount{
		Type:                   "connection_count",
		CurrentConnectionCount: h.ConnectionCount(ownerID),
	})
}

func (h *Hub) broadcast(ownerID string, payload any) {
	h.mu.RLock()
	connections := h.connections[ownerID]
	if len(connections) == 0 {
		h.mu.RUnlock()
		return
	}
	items := make([]*Connection, 0, len(connections))
	for conn := range connections {
		items = append(items, conn)
	}
	h.mu.RUnlock()
	for _, conn := range items {
		conn.Send(payload)
	}
}

type Connection struct {
	send func(any)
}

func NewConnection(send func(any)) *Connection {
	return &Connection{send: send}
}

func (c *Connection) Send(payload any) {
	if c == nil || c.send == nil {
		return
	}
	c.send(payload)
}

func ownerIDOfConversation(conversation common.Conversation) string {
	return stringValue(conversation.Metadata["owner_id"], "")
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}
