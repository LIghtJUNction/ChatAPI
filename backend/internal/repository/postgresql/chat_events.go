package postgresql

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) ListConversationEvents(ctx context.Context, conversationID string) ([]common.ConversationEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, conversation_id, owner_id, type, level, title, detail, request_id, metadata_json, created_at
		FROM conversation_events
		WHERE conversation_id = $1
		ORDER BY created_at ASC, id ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.ConversationEvent, 0)
	for rows.Next() {
		var item common.ConversationEvent
		var metadataJSON []byte
		if err := rows.Scan(
			&item.ID,
			&item.ConversationID,
			&item.OwnerID,
			&item.Type,
			&item.Level,
			&item.Title,
			&item.Detail,
			&item.RequestID,
			&metadataJSON,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.Metadata = parseJSONMap(metadataJSON)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AppendConversationEvent(ctx context.Context, input common.AppendConversationEventInput) (common.ConversationEvent, error) {
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	item := common.ConversationEvent{
		ID:             firstString(input.ID, "evt_"+uuid.NewString()),
		ConversationID: input.ConversationID,
		OwnerID:        input.OwnerID,
		Type:           input.Type,
		Level:          firstString(input.Level, "info"),
		Title:          input.Title,
		Detail:         input.Detail,
		RequestID:      input.RequestID,
		Metadata:       ensureMap(input.Metadata),
		CreatedAt:      createdAt,
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO conversation_events(
			id, conversation_id, owner_id, type, level, title, detail, request_id, metadata_json, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)
	`, item.ID, item.ConversationID, item.OwnerID, item.Type, item.Level, item.Title, item.Detail, item.RequestID, mustJSON(item.Metadata), item.CreatedAt); err != nil {
		return common.ConversationEvent{}, err
	}
	return item, nil
}

func firstString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
