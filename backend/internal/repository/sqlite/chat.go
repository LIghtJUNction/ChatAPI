package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/zyf/chatapi/internal/store"
)

func (s *Store) ListConversations(ctx context.Context) ([]store.Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(json_extract(metadata_json, '$.response_id'), '')
		FROM conversations
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.Conversation, 0)
	for rows.Next() {
		var item store.Conversation
		var createdAt, updatedAt, lastMessageAt string
		var metadataJSON string
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&createdAt,
			&updatedAt,
			&lastMessageAt,
			&item.MessageCount,
			&item.LastMessagePreview,
			&item.LastUserText,
			&metadataJSON,
			&item.ResponseID,
		); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		item.LastMessageAt = parseTime(lastMessageAt)
		item.Metadata = parseJSONMap(metadataJSON)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetConversation(ctx context.Context, conversationID string) (store.Conversation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(json_extract(metadata_json, '$.response_id'), '')
		FROM conversations
		WHERE id = ?
	`, conversationID)

	var item store.Conversation
	var createdAt, updatedAt, lastMessageAt string
	var metadataJSON string
	if err := row.Scan(
		&item.ID,
		&item.Title,
		&createdAt,
		&updatedAt,
		&lastMessageAt,
		&item.MessageCount,
		&item.LastMessagePreview,
		&item.LastUserText,
		&metadataJSON,
		&item.ResponseID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger(ctx).Warn("sqlite get conversation not found", zap.String("conversation.id", conversationID))
			return store.Conversation{}, errNotFound
		}
		s.logger(ctx).Warn("sqlite get conversation failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return store.Conversation{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	item.LastMessageAt = parseTime(lastMessageAt)
	item.Metadata = parseJSONMap(metadataJSON)
	return item, nil
}

func (s *Store) ListRequests(ctx context.Context) ([]store.Request, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND json_extract(m.metadata_json, '$.request_debug.request_id') IS NOT NULL
		ORDER BY c.updated_at DESC, m.created_at DESC, m.id DESC
	`)
	if err != nil {
		s.logger(ctx).Warn("sqlite list requests failed", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]store.Request, 0)
	for rows.Next() {
		item, err := scanRequestRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("sqlite list requests row iteration failed", zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) GetRequest(ctx context.Context, requestID string) (store.Request, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			m.conversation_id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND json_extract(m.metadata_json, '$.request_debug.request_id') = ?
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, requestID)

	item, err := scanRequestRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger(ctx).Warn("sqlite get request not found", zap.String("request.id", requestID))
			return store.Request{}, errNotFound
		}
		s.logger(ctx).Warn("sqlite get request failed", zap.String("request.id", requestID), zap.Error(err))
		return store.Request{}, err
	}
	if item.RequestID == "" {
		item.RequestID = requestID
	}
	return item, nil
}

func (s *Store) ListMessages(ctx context.Context, conversationID string) ([]store.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, role, content, created_at, status, response_id, metadata_json
		FROM messages
		WHERE conversation_id = ?
		ORDER BY created_at ASC, id ASC
	`, conversationID)
	if err != nil {
		s.logger(ctx).Warn("sqlite list messages failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]store.Message, 0)
	for rows.Next() {
		var item store.Message
		var createdAt string
		var status sql.NullString
		var responseID sql.NullString
		var metadataJSON string
		if err := rows.Scan(&item.ID, &item.Role, &item.Content, &createdAt, &status, &responseID, &metadataJSON); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.Status = status.String
		if responseID.Valid {
			item.ResponseID = &responseID.String
		}
		item.Metadata = parseJSONMap(metadataJSON)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("sqlite list messages row iteration failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) DeleteConversations(ctx context.Context, conversationIDs []string) (store.DeleteConversationsResult, error) {
	conversationIDs = uniqueNonEmptyStrings(conversationIDs)
	if len(conversationIDs) == 0 {
		return store.DeleteConversationsResult{}, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(conversationIDs)), ",")
	args := make([]any, 0, len(conversationIDs))
	for _, id := range conversationIDs {
		args = append(args, id)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.DeleteConversationsResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var result store.DeleteConversationsResult
	countMessagesQuery := fmt.Sprintf(`SELECT COUNT(*) FROM messages WHERE conversation_id IN (%s)`, placeholders)
	if err := tx.QueryRowContext(ctx, countMessagesQuery, args...).Scan(&result.DeletedMessages); err != nil {
		return store.DeleteConversationsResult{}, err
	}
	countAssetRefsQuery := fmt.Sprintf(`SELECT COUNT(*) FROM media_asset_refs WHERE conversation_id IN (%s)`, placeholders)
	if err := tx.QueryRowContext(ctx, countAssetRefsQuery, args...).Scan(&result.DeletedAssetRefs); err != nil {
		return store.DeleteConversationsResult{}, err
	}
	deleteQuery := fmt.Sprintf(`DELETE FROM conversations WHERE id IN (%s)`, placeholders)
	res, err := tx.ExecContext(ctx, deleteQuery, args...)
	if err != nil {
		return store.DeleteConversationsResult{}, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return store.DeleteConversationsResult{}, err
	}
	result.DeletedConversations = int(rowsAffected)
	if err := tx.Commit(); err != nil {
		return store.DeleteConversationsResult{}, err
	}
	return result, nil
}

func (s *Store) ExpirePendingTurns(ctx context.Context, cutoff time.Time) (store.ExpirePendingTurnsResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, metadata_json
		FROM conversations
		WHERE last_message_at < ?
			AND COALESCE(json_extract(metadata_json, '$.realtime_status'), '') IN ('waiting', 'streaming')
	`, formatTime(cutoff))
	if err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	type candidate struct {
		id       string
		metadata map[string]any
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		var metadataJSON string
		if err := rows.Scan(&item.id, &metadataJSON); err != nil {
			_ = rows.Close()
			return store.ExpirePendingTurnsResult{}, err
		}
		item.metadata = ensureMap(parseJSONMap(metadataJSON))
		item.metadata["realtime_status"] = "expired"
		item.metadata["realtime_draft_text"] = ""
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	if err := rows.Err(); err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	if len(candidates) == 0 {
		return store.ExpirePendingTurnsResult{}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	var result store.ExpirePendingTurnsResult
	for _, item := range candidates {
		res, err := tx.ExecContext(ctx, `
			UPDATE conversations
			SET updated_at = ?, metadata_json = ?
			WHERE id = ?
				AND COALESCE(json_extract(metadata_json, '$.realtime_status'), '') IN ('waiting', 'streaming')
		`, formatTime(now), mustJSON(item.metadata), item.id)
		if err != nil {
			return store.ExpirePendingTurnsResult{}, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return store.ExpirePendingTurnsResult{}, err
		}
		result.ExpiredConversations += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	return result, nil
}

func (s *Store) CreatePendingTurn(ctx context.Context, input store.CreatePendingInput) (store.Conversation, store.Message, error) {
	now := time.Now().UTC()
	metadata := map[string]any{
		"owner_id":            input.OwnerID,
		"request_format":      input.RequestFormat,
		"realtime_status":     "waiting",
		"realtime_draft_text": "",
		"response_id":         input.ResponseID,
		"model":               input.Model,
	}
	userMessageMetadata := map[string]any{
		"request_format": input.RequestFormat,
		"model":          input.Model,
		"request_debug": map[string]any{
			"request_id":      input.RequestID,
			"response_id":     input.ResponseID,
			"model":           input.Model,
			"request_format":  input.RequestFormat,
			"request_keys":    keysOf(input.RequestBody),
			"request_method":  input.RequestMethod,
			"request_path":    input.RequestPath,
			"request_query":   input.RequestQuery,
			"request_headers": input.RequestHeaders,
			"system_text":     input.SystemContent,
			"developer_text":  input.DeveloperContent,
			"assistant_text":  input.AssistantContent,
			"input_text":      input.UserContent,
			"input_parts":     input.InputParts,
			"request_body":    input.RequestBody,
			"tool_schemas":    input.ToolSchemas,
			"tool_choice":     input.ToolChoice,
			"response_format": input.ResponseFormat,
		},
	}
	conversation := store.Conversation{
		ID:                 input.ConversationID,
		Title:              buildConversationTitle(input.UserContent),
		LastUserText:       input.UserContent,
		CreatedAt:          now,
		UpdatedAt:          now,
		LastMessageAt:      now,
		MessageCount:       1,
		LastMessagePreview: input.UserContent,
		Metadata:           metadata,
		ResponseID:         input.ResponseID,
	}
	responseID := input.ResponseID
	message := store.Message{
		ID:         "msg_" + uuid.NewString(),
		Role:       "user",
		Content:    input.UserContent,
		CreatedAt:  now,
		Status:     "pending",
		ResponseID: &responseID,
		Metadata:   userMessageMetadata,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger(ctx).Warn("sqlite create pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO conversations(
			id, title, created_at, updated_at, last_message_at,
			message_count, last_message_preview, last_user_text, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		conversation.ID,
		conversation.Title,
		formatTime(now),
		formatTime(now),
		formatTime(now),
		conversation.MessageCount,
		conversation.LastMessagePreview,
		conversation.LastUserText,
		mustJSON(metadata),
	); err != nil {
		return store.Conversation{}, store.Message{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		message.ID,
		conversation.ID,
		message.Role,
		message.Content,
		formatTime(now),
		message.Status,
		responseID,
		mustJSON(userMessageMetadata),
	); err != nil {
		return store.Conversation{}, store.Message{}, err
	}

	for _, asset := range input.PreparedImages {
		assetID := "asset_" + uuid.NewString()
		refID := "assetref_" + uuid.NewString()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO media_assets(
				id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			assetID,
			input.OwnerID,
			asset.FileID,
			asset.Path,
			asset.MediaType,
			asset.Bytes,
			asset.SHA256,
			asset.Width,
			asset.Height,
			asset.SourceKind,
			asset.OriginalName,
			asset.OriginalMediaType,
			formatTime(now),
		); err != nil {
			return store.Conversation{}, store.Message{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO media_asset_refs(
				id, asset_id, file_id, owner_id, request_id, conversation_id, message_id, input_part_index, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			refID,
			assetID,
			asset.FileID,
			input.OwnerID,
			input.RequestID,
			conversation.ID,
			message.ID,
			asset.InputPartIndex,
			formatTime(now),
		); err != nil {
			return store.Conversation{}, store.Message{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		s.logger(ctx).Warn("sqlite create pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	s.logger(ctx).Debug("sqlite pending turn created", zap.String("conversation.id", input.ConversationID), zap.String("request.id", input.RequestID), zap.String("owner.id", input.OwnerID))
	return conversation, message, nil
}

func (s *Store) ListMediaAssets(ctx context.Context) ([]store.MediaAsset, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at
		FROM media_assets
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.MediaAsset, 0)
	for rows.Next() {
		item, err := scanMediaAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListOrphanMediaAssets(ctx context.Context) ([]store.MediaAsset, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.owner_id, a.file_id, a.path, a.media_type, a.bytes, a.sha256, a.width, a.height, a.source_kind, a.original_name, a.original_media_type, a.created_at
		FROM media_assets a
		LEFT JOIN media_asset_refs r ON r.asset_id = a.id
		WHERE r.id IS NULL
		ORDER BY a.created_at ASC, a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.MediaAsset, 0)
	for rows.Next() {
		item, err := scanMediaAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteMediaAssetsByIDs(ctx context.Context, ids []string) (int, error) {
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	query := fmt.Sprintf(`DELETE FROM media_assets WHERE id IN (%s)`, placeholders)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(rowsAffected), nil
}

func (s *Store) UpdateDraft(ctx context.Context, input store.UpdateDraftInput) (store.Conversation, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isDraftWritable(metadata) {
		return store.Conversation{}, errConflict
	}
	metadata["realtime_draft_text"] = input.DraftText
	metadata["realtime_status"] = "streaming"
	conversation.Metadata = metadata
	conversation.UpdatedAt = time.Now().UTC()

	if _, err := s.db.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(conversation.UpdatedAt), mustJSON(metadata), conversation.ID); err != nil {
		s.logger(ctx).Warn("sqlite update draft failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, err
	}
	s.logger(ctx).Debug("sqlite draft updated", zap.String("conversation.id", input.ConversationID), zap.Int("draft.length", len([]rune(input.DraftText))))
	return conversation, nil
}

func (s *Store) CompletePendingTurn(ctx context.Context, input store.CompletePendingInput) (store.Conversation, store.Message, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isTurnCompletable(metadata) {
		return store.Conversation{}, store.Message{}, errConflict
	}
	draftText, _ := metadata["realtime_draft_text"].(string)
	finalText := strings.TrimSpace(input.OutputText)
	if finalText == "" {
		finalText = draftText
	}
	messageContent := finalText
	if input.Mode == "thinking" && finalText != "" {
		messageContent = "<think>" + finalText + "</think>"
	}
	now := time.Now().UTC()
	metadata["realtime_status"] = "closed"
	metadata["realtime_draft_text"] = ""

	messageMetadata := map[string]any{
		"response_mode": input.Mode,
	}
	if input.ToolName != "" {
		messageMetadata["tool_name"] = input.ToolName
	}
	if input.ToolCallID != "" {
		messageMetadata["tool_call_id"] = input.ToolCallID
	}
	if input.Mode == "tool_call" {
		messageMetadata["arguments"] = finalText
	}
	if input.Mode == "tool_result" {
		messageMetadata["output"] = stringValue(input.ToolOutput, finalText)
	}
	if input.ReasoningStreamMode != "" {
		messageMetadata["reasoning_stream_mode"] = input.ReasoningStreamMode
	}
	responseID := input.ResponseID
	message := store.Message{
		ID:         "msg_" + uuid.NewString(),
		Role:       "assistant",
		Content:    messageContent,
		CreatedAt:  now,
		Status:     "completed",
		ResponseID: &responseID,
		Metadata:   messageMetadata,
	}

	conversation.Metadata = metadata
	conversation.UpdatedAt = now
	conversation.LastMessageAt = now
	conversation.MessageCount += 1
	conversation.LastMessagePreview = finalText

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger(ctx).Warn("sqlite complete pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, message.ID, conversation.ID, message.Role, message.Content, formatTime(now), message.Status, responseID, mustJSON(messageMetadata)); err != nil {
		return store.Conversation{}, store.Message{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, last_message_at = ?, message_count = ?, last_message_preview = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(now), formatTime(now), conversation.MessageCount, conversation.LastMessagePreview, mustJSON(metadata), conversation.ID); err != nil {
		return store.Conversation{}, store.Message{}, err
	}

	if err := tx.Commit(); err != nil {
		s.logger(ctx).Warn("sqlite complete pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	s.logger(ctx).Debug("sqlite pending turn completed", zap.String("conversation.id", input.ConversationID), zap.String("response.id", input.ResponseID), zap.String("mode", input.Mode))
	return conversation, message, nil
}

func (s *Store) AbortPendingTurn(ctx context.Context, input store.AbortPendingInput) (store.Conversation, store.Message, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isTurnCompletable(metadata) {
		return store.Conversation{}, store.Message{}, errConflict
	}
	metadata["realtime_status"] = "aborted"
	metadata["realtime_draft_text"] = ""
	now := time.Now().UTC()

	message := store.Message{
		ID:        "msg_" + uuid.NewString(),
		Role:      "assistant",
		Content:   input.Reason,
		CreatedAt: now,
		Status:    "aborted",
		Metadata: map[string]any{
			"response_mode": "assistant_message",
		},
	}
	conversation.Metadata = metadata
	conversation.UpdatedAt = now
	conversation.LastMessageAt = now
	conversation.MessageCount += 1
	conversation.LastMessagePreview = input.Reason

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger(ctx).Warn("sqlite abort pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, message.ID, conversation.ID, message.Role, message.Content, formatTime(now), message.Status, nil, mustJSON(message.Metadata)); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, last_message_at = ?, message_count = ?, last_message_preview = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(now), formatTime(now), conversation.MessageCount, conversation.LastMessagePreview, mustJSON(metadata), conversation.ID); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	if err := tx.Commit(); err != nil {
		s.logger(ctx).Warn("sqlite abort pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	s.logger(ctx).Debug("sqlite pending turn aborted", zap.String("conversation.id", input.ConversationID))
	return conversation, message, nil
}
