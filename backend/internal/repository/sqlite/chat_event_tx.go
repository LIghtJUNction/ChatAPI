package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func buildConversationEventFromInput(conversation common.Conversation, input common.AppendConversationEventInput, fallbackTime time.Time) common.ConversationEvent {
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		if fallbackTime.IsZero() {
			createdAt = nowUTC()
		} else {
			createdAt = fallbackTime.UTC()
		}
	}
	return common.ConversationEvent{
		ID:             stringValue(input.ID, "evt_"+uuid.NewString()),
		ConversationID: stringValue(input.ConversationID, conversation.ID),
		OwnerID:        stringValue(input.OwnerID, metadataString(conversation.Metadata, "owner_id", "")),
		Type:           input.Type,
		Level:          stringValue(input.Level, "info"),
		Title:          input.Title,
		Detail:         input.Detail,
		RequestID:      input.RequestID,
		Metadata:       ensureMap(input.Metadata),
		CreatedAt:      createdAt,
	}
}

func insertConversationEventSQLite(ctx context.Context, tx *sql.Tx, item common.ConversationEvent) error {
	if tx == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO conversation_events(
			id, conversation_id, owner_id, type, level, title, detail, request_id, metadata_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.ConversationID, item.OwnerID, item.Type, item.Level, item.Title, item.Detail, item.RequestID, mustJSON(item.Metadata), formatTime(item.CreatedAt))
	return err
}
