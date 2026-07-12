package timeline

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"go.uber.org/zap"
)

type Item struct {
	ID        string                    `json:"id"`
	Kind      string                    `json:"kind"`
	CreatedAt time.Time                 `json:"created_at"`
	Message   *common.Message           `json:"message,omitempty"`
	Event     *common.ConversationEvent `json:"event,omitempty"`
}

type Service struct {
	store  chat.Store
	logger *zap.Logger
}

func New(store chat.Store, logger *zap.Logger) *Service {
	return &Service{store: store, logger: logger}
}

func (s *Service) ListTimeline(ctx context.Context, conversationID string) ([]Item, error) {
	conversationID = strings.TrimSpace(conversationID)
	messages, err := s.store.ListMessages(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	events, err := s.store.ListConversationEvents(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(messages)+len(events))
	for i := range messages {
		message := messages[i]
		items = append(items, ItemFromMessage(message))
	}
	for i := range events {
		event := events[i]
		items = append(items, ItemFromConversationEvent(event))
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left.CreatedAt.Equal(right.CreatedAt) {
			return left.ID < right.ID
		}
		return left.CreatedAt.Before(right.CreatedAt)
	})
	return items, nil
}

func ItemFromMessage(message common.Message) Item {
	return Item{
		ID:        "msg:" + message.ID,
		Kind:      "message",
		CreatedAt: message.CreatedAt,
		Message:   &message,
	}
}

func ItemFromConversationEvent(event common.ConversationEvent) Item {
	return Item{
		ID:        "evt:" + event.ID,
		Kind:      "system_event",
		CreatedAt: event.CreatedAt,
		Event:     &event,
	}
}
