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
	ResponseID         string         `json:"-"`
}

type Message struct {
	ID         string         `json:"id"`
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	CreatedAt  time.Time      `json:"created_at"`
	Status     string         `json:"status,omitempty"`
	ResponseID *string        `json:"response_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type CreatePendingInput struct {
	ConversationID string
	RequestID      string
	ResponseID     string
	RequestFormat  string
	Model          string
	UserContent    string
	RequestBody    map[string]any
	ToolSchemas    []any
}

type CompletePendingInput struct {
	ConversationID      string
	ResponseID          string
	OutputText          string
	Mode                string
	ToolName            string
	ToolCallID          string
	ReasoningStreamMode string
}

type UpdateDraftInput struct {
	ConversationID string
	DraftText      string
}

type AbortPendingInput struct {
	ConversationID string
	Reason         string
}

type Store interface {
	Ping(context.Context) error
	ListConversations(context.Context) ([]Conversation, error)
	GetConversation(context.Context, string) (Conversation, error)
	ListMessages(context.Context, string) ([]Message, error)
	CreatePendingTurn(context.Context, CreatePendingInput) (Conversation, Message, error)
	UpdateDraft(context.Context, UpdateDraftInput) (Conversation, error)
	CompletePendingTurn(context.Context, CompletePendingInput) (Conversation, Message, error)
	AbortPendingTurn(context.Context, AbortPendingInput) (Conversation, Message, error)
}
