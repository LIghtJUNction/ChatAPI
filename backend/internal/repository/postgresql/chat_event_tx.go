package postgresql

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func insertConversationEventPostgreSQL(ctx context.Context, tx pgx.Tx, conversation common.Conversation, input *common.AppendConversationEventInput, fallbackTime time.Time) error {
	if tx == nil || input == nil {
		return nil
	}
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		if fallbackTime.IsZero() {
			createdAt = time.Now().UTC()
		} else {
			createdAt = fallbackTime.UTC()
		}
	}
	item := common.ConversationEvent{
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
	_, err := tx.Exec(ctx, `
		INSERT INTO conversation_events(
			id, conversation_id, owner_id, type, level, title, detail, request_id, metadata_json, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)
	`, item.ID, item.ConversationID, item.OwnerID, item.Type, item.Level, item.Title, item.Detail, item.RequestID, mustJSON(item.Metadata), item.CreatedAt)
	return err
}
