package conversationstate

import (
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type Status string

const (
	StatusWaiting      Status = "waiting"
	StatusStreaming    Status = "streaming"
	StatusClosed       Status = "closed"
	StatusAborted      Status = "aborted"
	StatusDisconnected Status = "disconnected"
	StatusExpired      Status = "expired"
)

type Runtime struct {
	OwnerID       string
	RequestID     string
	RequestFormat string
	Model         string
	Status        Status
	DraftText     string
}

type Summary struct {
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	LastUserText       string    `json:"last_user_text"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	LastMessageAt      time.Time `json:"last_message_at"`
	MessageCount       int       `json:"message_count"`
	LastMessagePreview string    `json:"last_message_preview"`
	RequestFormat      string    `json:"request_format,omitempty"`
	RequestID          string    `json:"request_id,omitempty"`
	Status             Status    `json:"status,omitempty"`
	DraftText          string    `json:"draft_text,omitempty"`
}

func FromConversation(conversation common.Conversation) Runtime {
	return Runtime{
		OwnerID:       metadataString(conversation.Metadata, "owner_id"),
		RequestID:     metadataString(conversation.Metadata, "request_id"),
		RequestFormat: metadataString(conversation.Metadata, "request_format"),
		Model:         metadataString(conversation.Metadata, "model"),
		Status:        Status(metadataString(conversation.Metadata, "realtime_status")),
		DraftText:     metadataText(conversation.Metadata, "realtime_draft_text"),
	}
}

func SummaryFromConversation(conversation common.Conversation) Summary {
	runtime := FromConversation(conversation)
	return Summary{
		ID:                 conversation.ID,
		Title:              conversation.Title,
		LastUserText:       conversation.LastUserText,
		CreatedAt:          conversation.CreatedAt,
		UpdatedAt:          conversation.UpdatedAt,
		LastMessageAt:      conversation.LastMessageAt,
		MessageCount:       conversation.MessageCount,
		LastMessagePreview: conversation.LastMessagePreview,
		RequestFormat:      runtime.RequestFormat,
		RequestID:          runtime.RequestID,
		Status:             runtime.Status,
		DraftText:          runtime.DraftText,
	}
}

func RequestID(conversation common.Conversation) string {
	return FromConversation(conversation).RequestID
}

func OwnerID(conversation common.Conversation) string {
	return FromConversation(conversation).OwnerID
}

func RequestFormat(conversation common.Conversation) string {
	value := RequestFormatRaw(conversation)
	if value == "" {
		return string(protocol.ProtocolResponses)
	}
	return value
}

func RequestFormatRaw(conversation common.Conversation) string {
	return FromConversation(conversation).RequestFormat
}

func Model(conversation common.Conversation, fallback string) string {
	value := FromConversation(conversation).Model
	if value == "" {
		return strings.TrimSpace(fallback)
	}
	return value
}

func IsPendingStatus(status Status) bool {
	return status == StatusWaiting || status == StatusStreaming
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func metadataText(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}
