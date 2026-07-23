package events

import (
	"context"
	"strings"
	"sync"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type Type string

const (
	TypeConversationUpserted      Type = "conversation.upserted"
	TypeConversationDeleted       Type = "conversation.deleted"
	TypeMessageAppended           Type = "message.appended"
	TypeConversationEventAppended Type = "conversation_event.appended"
	TypeTurnWaiting               Type = "turn.waiting"
)

type WaitingTurn struct {
	OwnerID        string `json:"owner_id"`
	RequestID      string `json:"request_id"`
	ResponseID     string `json:"response_id"`
	ConversationID string `json:"conversation_id"`
	Protocol       string `json:"protocol"`
	Model          string `json:"model"`
	LastUserText   string `json:"last_user_text"`
}

type Event struct {
	Type              Type
	OwnerID           string
	ConversationID    string
	RequestID         string
	ControlManaged    bool
	Conversation      common.Conversation
	Message           *common.Message
	ConversationEvent *common.ConversationEvent
	WaitingTurn       *WaitingTurn
	SkipCountRefresh  bool
}

type Publisher interface {
	Publish(context.Context, Event)
}

type Subscriber interface {
	HandleChatEvent(context.Context, Event)
}

type Dispatcher struct {
	mu          sync.RWMutex
	subscribers []Subscriber
}

func NewDispatcher(subscribers ...Subscriber) *Dispatcher {
	d := &Dispatcher{}
	for _, subscriber := range subscribers {
		d.Subscribe(subscriber)
	}
	return d
}

func (d *Dispatcher) Subscribe(subscriber Subscriber) {
	if d == nil || subscriber == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subscribers = append(d.subscribers, subscriber)
}

func (d *Dispatcher) Publish(ctx context.Context, event Event) {
	if d == nil {
		return
	}
	d.mu.RLock()
	subscribers := append([]Subscriber(nil), d.subscribers...)
	d.mu.RUnlock()
	for _, subscriber := range subscribers {
		subscriber.HandleChatEvent(ctx, event)
	}
}

type NoopPublisher struct{}

func (NoopPublisher) Publish(context.Context, Event) {}

func PublishDeletedConversations(ctx context.Context, publisher Publisher, result common.DeleteConversationsResult) {
	if publisher == nil {
		return
	}
	lastByOwner := make(map[string]int)
	for index, item := range result.DeletedConversationItems {
		ownerID := strings.TrimSpace(item.OwnerID)
		if ownerID != "" {
			lastByOwner[ownerID] = index
		}
	}
	for index, item := range result.DeletedConversationItems {
		ownerID := strings.TrimSpace(item.OwnerID)
		conversationID := strings.TrimSpace(item.ID)
		if ownerID == "" || conversationID == "" {
			continue
		}
		publisher.Publish(ctx, Event{
			Type:             TypeConversationDeleted,
			OwnerID:          ownerID,
			ConversationID:   conversationID,
			SkipCountRefresh: lastByOwner[ownerID] != index,
		})
	}
}
