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
)

type Event struct {
	Type              Type
	OwnerID           string
	ConversationID    string
	Conversation      common.Conversation
	Message           *common.Message
	ConversationEvent *common.ConversationEvent
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
	for _, item := range result.DeletedConversationItems {
		ownerID := strings.TrimSpace(item.OwnerID)
		conversationID := strings.TrimSpace(item.ID)
		if ownerID == "" || conversationID == "" {
			continue
		}
		publisher.Publish(ctx, Event{
			Type:           TypeConversationDeleted,
			OwnerID:        ownerID,
			ConversationID: conversationID,
		})
	}
}
