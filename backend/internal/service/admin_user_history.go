package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

type AdminUserHistoryService struct {
	store store.Store
}

type AdminUserHistoryMessage struct {
	ID                string         `json:"id"`
	ConversationID    string         `json:"conversation_id"`
	ConversationTitle string         `json:"conversation_title"`
	Role              string         `json:"role"`
	Content           string         `json:"content"`
	Status            string         `json:"status,omitempty"`
	ResponseID        *string        `json:"response_id,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
}

func NewAdminUserHistoryService(dataStore store.Store) *AdminUserHistoryService {
	return &AdminUserHistoryService{store: dataStore}
}

func (s *AdminUserHistoryService) Get(ctx context.Context, userID string, limit int) (store.User, []AdminUserHistoryMessage, error) {
	if s == nil || s.store == nil {
		return store.User{}, nil, ErrInvalidUserInput
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return store.User{}, nil, ErrInvalidUserInput
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}

	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return store.User{}, nil, err
	}
	conversations, err := s.store.ListConversations(ctx)
	if err != nil {
		return store.User{}, nil, err
	}

	history := make([]AdminUserHistoryMessage, 0, limit)
	for _, conversation := range conversations {
		if stringValue(conversation.Metadata["owner_id"], "") != userID {
			continue
		}
		messages, err := s.store.ListMessages(ctx, conversation.ID)
		if err != nil {
			return store.User{}, nil, err
		}
		for _, message := range messages {
			history = append(history, AdminUserHistoryMessage{
				ID:                message.ID,
				ConversationID:    conversation.ID,
				ConversationTitle: conversation.Title,
				Role:              message.Role,
				Content:           message.Content,
				Status:            message.Status,
				ResponseID:        message.ResponseID,
				Metadata:          message.Metadata,
				CreatedAt:         message.CreatedAt,
			})
		}
	}

	sort.SliceStable(history, func(i, j int) bool {
		if history[i].CreatedAt.Equal(history[j].CreatedAt) {
			return history[i].ID > history[j].ID
		}
		return history[i].CreatedAt.After(history[j].CreatedAt)
	})
	if len(history) > limit {
		history = history[:limit]
	}
	return user, history, nil
}
