package postgresql

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func buildConversationEventFromInput(conversation common.Conversation, input common.AppendConversationEventInput, fallbackTime time.Time) common.ConversationEvent {
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		if fallbackTime.IsZero() {
			createdAt = time.Now().UTC()
		} else {
			createdAt = fallbackTime.UTC()
		}
	}
	return common.ConversationEvent{
		ID:             firstString(input.ID, "evt_"+uuid.NewString()),
		ConversationID: firstString(input.ConversationID, conversation.ID),
		OwnerID:        firstString(input.OwnerID, metadataString(conversation.Metadata, "owner_id", "")),
		Type:           input.Type,
		Level:          firstString(input.Level, "info"),
		Title:          input.Title,
		Detail:         input.Detail,
		RequestID:      input.RequestID,
		Metadata:       ensureMap(input.Metadata),
		CreatedAt:      createdAt,
	}
}

func insertConversationEventPostgreSQL(ctx context.Context, tx pgx.Tx, item common.ConversationEvent) error {
	if tx == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO conversation_events(
			id, conversation_id, owner_id, type, level, title, detail, request_id, metadata_json, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)
	`, item.ID, item.ConversationID, item.OwnerID, item.Type, item.Level, item.Title, item.Detail, item.RequestID, mustJSON(item.Metadata), item.CreatedAt)
	return err
}
