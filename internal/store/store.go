package store

import (
	"context"
	"time"
)

type Conversation struct {
	ID                 string         `json:"id"`
	Title              string         `json:"title"`
	LastUserText       string         `json:"last_user_text"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	LastMessageAt      time.Time      `json:"last_message_at"`
	MessageCount       int            `json:"message_count"`
	LastMessagePreview string         `json:"last_message_preview"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type Store interface {
	Ping(context.Context) error
	ListConversations(context.Context) ([]Conversation, error)
}
