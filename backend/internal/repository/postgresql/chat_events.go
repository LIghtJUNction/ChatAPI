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
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	assetRows, err := s.pool.Query(ctx, `
		SELECT er.id, er.event_id, er.asset_id, er.file_id, er.url,
		       a.media_type, a.bytes, a.width, a.height, er.purpose, er.part_index
		FROM media_asset_event_refs er
		JOIN media_assets a ON a.id = er.asset_id
		WHERE er.conversation_id = $1
		ORDER BY er.event_id, er.part_index, er.id
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer assetRows.Close()
	assetsByEvent := make(map[string][]common.EventMediaAssetRef)
	for assetRows.Next() {
		var eventID string
		var ref common.EventMediaAssetRef
		if err := assetRows.Scan(&ref.ID, &eventID, &ref.AssetID, &ref.FileID, &ref.URL, &ref.MediaType, &ref.Bytes, &ref.Width, &ref.Height, &ref.Purpose, &ref.PartIndex); err != nil {
			return nil, err
		}
		assetsByEvent[eventID] = append(assetsByEvent[eventID], ref)
	}
	if err := assetRows.Err(); err != nil {
		return nil, err
	}
	for index := range items {
		items[index].MediaAssets = assetsByEvent[items[index].ID]
	}
	return items, nil
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

func (s *Store) AppendConversationEventWithAsset(ctx context.Context, input common.AppendConversationEventWithAssetInput) (common.ConversationEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return common.ConversationEvent{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var asset common.MediaAsset
	err = tx.QueryRow(ctx, `
		SELECT a.id, a.owner_id, a.file_id, a.path, a.media_type, a.bytes, a.sha256, a.width, a.height, a.source_kind, a.original_name, a.original_media_type, a.created_at
		FROM media_assets a
		JOIN media_asset_staging st ON st.asset_id = a.id
		JOIN conversations c ON c.id = st.conversation_id
		WHERE a.id = $1 AND st.owner_id = $2 AND st.conversation_id = $3 AND st.request_id = $4
		  AND COALESCE(c.metadata_json->>'realtime_status', '') IN ('waiting', 'streaming')
		  AND COALESCE(c.metadata_json->>'request_id', '') = st.request_id
	`, input.AssetID, input.Event.OwnerID, input.Event.ConversationID, input.Event.RequestID).Scan(&asset.ID, &asset.OwnerID, &asset.FileID, &asset.Path, &asset.MediaType, &asset.Bytes, &asset.SHA256, &asset.Width, &asset.Height, &asset.SourceKind, &asset.OriginalName, &asset.OriginalMediaType, &asset.CreatedAt)
	if err != nil {
		return common.ConversationEvent{}, err
	}
	if asset.OwnerID != input.Event.OwnerID {
		return common.ConversationEvent{}, common.ErrNotFound
	}
	event := buildConversationEventFromInput(common.Conversation{}, input.Event, time.Now().UTC())
	if err := insertConversationEventPostgreSQL(ctx, tx, event); err != nil {
		return common.ConversationEvent{}, err
	}
	assetRefID := "assetref_" + uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO media_asset_event_refs(
			id, asset_id, file_id, url, owner_id, request_id, conversation_id, event_id, purpose, part_index, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 0, $10)
	`, assetRefID, asset.ID, asset.FileID, input.AssetURL, asset.OwnerID, event.RequestID, event.ConversationID, event.ID, firstString(input.Purpose, "image_generation_result"), event.CreatedAt)
	if err != nil {
		return common.ConversationEvent{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM media_asset_staging WHERE asset_id = $1`, asset.ID); err != nil {
		return common.ConversationEvent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return common.ConversationEvent{}, err
	}
	event.MediaAssets = []common.EventMediaAssetRef{{
		ID: assetRefID, AssetID: asset.ID, FileID: asset.FileID, URL: input.AssetURL, MediaType: asset.MediaType,
		Bytes: asset.Bytes, Width: asset.Width, Height: asset.Height,
		Purpose: firstString(input.Purpose, "image_generation_result"), PartIndex: 0,
	}}
	return event, nil
}

func firstString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
