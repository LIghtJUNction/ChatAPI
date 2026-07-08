package sqlite

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) ListConversationEvents(ctx context.Context, conversationID string) ([]common.ConversationEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, conversation_id, owner_id, type, level, title, detail, request_id, metadata_json, created_at
		FROM conversation_events
		WHERE conversation_id = ?
		ORDER BY created_at ASC, id ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.ConversationEvent, 0)
	for rows.Next() {
		var item common.ConversationEvent
		var metadataJSON string
		var createdAt string
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
			&createdAt,
		); err != nil {
			return nil, err
		}
		item.Metadata = parseJSONMap(metadataJSON)
		item.CreatedAt = parseTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AppendConversationEvent(ctx context.Context, input common.AppendConversationEventInput) (common.ConversationEvent, error) {
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = nowUTC()
	}
	item := common.ConversationEvent{
		ID:             stringValue(input.ID, "evt_"+uuid.NewString()),
		ConversationID: input.ConversationID,
		OwnerID:        input.OwnerID,
		Type:           input.Type,
		Level:          stringValue(input.Level, "info"),
		Title:          input.Title,
		Detail:         input.Detail,
		RequestID:      input.RequestID,
		Metadata:       ensureMap(input.Metadata),
		CreatedAt:      createdAt,
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO conversation_events(
			id, conversation_id, owner_id, type, level, title, detail, request_id, metadata_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.ConversationID, item.OwnerID, item.Type, item.Level, item.Title, item.Detail, item.RequestID, mustJSON(item.Metadata), formatTime(item.CreatedAt)); err != nil {
		return common.ConversationEvent{}, err
	}
	return item, nil
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
