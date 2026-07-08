package postgresql

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/zyf/chatapi/internal/store"
)

func (s *Store) ListConversations(ctx context.Context) ([]store.Conversation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(metadata_json->>'response_id', '')
		FROM conversations
		ORDER BY updated_at DESC
	`)
	if err != nil {
		s.logger(ctx).Warn("postgresql list requests failed", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]store.Conversation, 0)
	for rows.Next() {
		item, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("postgresql list requests row iteration failed", zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) GetConversation(ctx context.Context, conversationID string) (store.Conversation, error) {
	return scanConversation(s.pool.QueryRow(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(metadata_json->>'response_id', '')
		FROM conversations
		WHERE id = $1
	`, strings.TrimSpace(conversationID)))
}

func (s *Store) ListRequests(ctx context.Context) ([]store.Request, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			c.id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND m.metadata_json->'request_debug'->>'request_id' IS NOT NULL
		ORDER BY c.updated_at DESC, m.created_at DESC, m.id DESC
	`)
	if err != nil {
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
	return items, rows.Err()
}

func (s *Store) GetRequest(ctx context.Context, requestID string) (store.Request, error) {
	item, err := scanRequestRow(s.pool.QueryRow(ctx, `
		SELECT
			m.conversation_id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND m.metadata_json->'request_debug'->>'request_id' = $1
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, strings.TrimSpace(requestID)))
	if err != nil {
		s.logger(ctx).Warn("postgresql get request failed", zap.String("request.id", requestID), zap.Error(err))
		return store.Request{}, err
	}
	if item.RequestID == "" {
		item.RequestID = strings.TrimSpace(requestID)
	}
	return item, nil
}

func (s *Store) ListMessages(ctx context.Context, conversationID string) ([]store.Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, role, content, created_at, status, response_id, metadata_json
		FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC, id ASC
	`, strings.TrimSpace(conversationID))
	if err != nil {
		s.logger(ctx).Warn("postgresql list messages failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]store.Message, 0)
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("postgresql list messages row iteration failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) DeleteConversations(ctx context.Context, conversationIDs []string) (store.DeleteConversationsResult, error) {
	conversationIDs = uniqueNonEmptyStrings(conversationIDs)
	if len(conversationIDs) == 0 {
		return store.DeleteConversationsResult{}, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.DeleteConversationsResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var result store.DeleteConversationsResult
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE conversation_id = ANY($1)`, conversationIDs).Scan(&result.DeletedMessages); err != nil {
		return store.DeleteConversationsResult{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM media_asset_refs WHERE conversation_id = ANY($1)`, conversationIDs).Scan(&result.DeletedAssetRefs); err != nil {
		return store.DeleteConversationsResult{}, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM conversations WHERE id = ANY($1)`, conversationIDs)
	if err != nil {
		return store.DeleteConversationsResult{}, err
	}
	result.DeletedConversations = int(tag.RowsAffected())
	if err := tx.Commit(ctx); err != nil {
		return store.DeleteConversationsResult{}, err
	}
	return result, nil
}

func (s *Store) ExpirePendingTurns(ctx context.Context, cutoff time.Time) (store.ExpirePendingTurnsResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, metadata_json
		FROM conversations
		WHERE last_message_at < $1
			AND COALESCE(metadata_json->>'realtime_status', '') IN ('waiting', 'streaming')
	`, cutoff)
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
		var metadataJSON []byte
		if err := rows.Scan(&item.id, &metadataJSON); err != nil {
			rows.Close()
			return store.ExpirePendingTurnsResult{}, err
		}
		item.metadata = ensureMap(parseJSONMap(metadataJSON))
		item.metadata["realtime_status"] = "expired"
		item.metadata["realtime_draft_text"] = ""
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	if len(candidates) == 0 {
		return store.ExpirePendingTurnsResult{}, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now().UTC()
	var result store.ExpirePendingTurnsResult
	for _, item := range candidates {
		tag, err := tx.Exec(ctx, `
			UPDATE conversations
			SET updated_at = $1, metadata_json = $2::jsonb
			WHERE id = $3
				AND COALESCE(metadata_json->>'realtime_status', '') IN ('waiting', 'streaming')
		`, now, mustJSON(item.metadata), item.id)
		if err != nil {
			return store.ExpirePendingTurnsResult{}, err
		}
		result.ExpiredConversations += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	return result, nil
}

func (s *Store) CreatePendingTurn(ctx context.Context, input store.CreatePendingInput) (store.Conversation, store.Message, error) {
	now := time.Now().UTC()
	metadata := map[string]any{
		"owner_id":            strings.TrimSpace(input.OwnerID),
		"request_format":      strings.TrimSpace(input.RequestFormat),
		"realtime_status":     "waiting",
		"realtime_draft_text": "",
		"response_id":         strings.TrimSpace(input.ResponseID),
		"model":               strings.TrimSpace(input.Model),
	}
	userMessageMetadata := map[string]any{
		"request_format": strings.TrimSpace(input.RequestFormat),
		"model":          strings.TrimSpace(input.Model),
		"request_debug": map[string]any{
			"request_id":      strings.TrimSpace(input.RequestID),
			"response_id":     strings.TrimSpace(input.ResponseID),
			"model":           strings.TrimSpace(input.Model),
			"request_format":  strings.TrimSpace(input.RequestFormat),
			"request_keys":    keysOf(input.RequestBody),
			"request_method":  strings.TrimSpace(input.RequestMethod),
			"request_path":    strings.TrimSpace(input.RequestPath),
			"request_query":   input.RequestQuery,
			"request_headers": input.RequestHeaders,
			"system_text":     strings.TrimSpace(input.SystemContent),
			"developer_text":  strings.TrimSpace(input.DeveloperContent),
			"assistant_text":  strings.TrimSpace(input.AssistantContent),
			"input_text":      input.UserContent,
			"input_parts":     input.InputParts,
			"request_body":    input.RequestBody,
			"tool_schemas":    input.ToolSchemas,
			"tool_choice":     input.ToolChoice,
			"response_format": input.ResponseFormat,
		},
	}
	conversation := store.Conversation{
		ID:                 strings.TrimSpace(input.ConversationID),
		Title:              buildConversationTitle(input.UserContent),
		LastUserText:       input.UserContent,
		CreatedAt:          now,
		UpdatedAt:          now,
		LastMessageAt:      now,
		MessageCount:       1,
		LastMessagePreview: input.UserContent,
		Metadata:           metadata,
		ResponseID:         strings.TrimSpace(input.ResponseID),
	}
	responseID := strings.TrimSpace(input.ResponseID)
	message := store.Message{
		ID:         "msg_" + uuid.NewString(),
		Role:       "user",
		Content:    input.UserContent,
		CreatedAt:  now,
		Status:     "pending",
		ResponseID: &responseID,
		Metadata:   userMessageMetadata,
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.logger(ctx).Warn("postgresql create pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO conversations(
			id, title, created_at, updated_at, last_message_at,
			message_count, last_message_preview, last_user_text, metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
	`,
		conversation.ID,
		conversation.Title,
		now,
		now,
		now,
		conversation.MessageCount,
		conversation.LastMessagePreview,
		conversation.LastUserText,
		mustJSON(metadata),
	); err != nil {
		return store.Conversation{}, store.Message{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
	`,
		message.ID,
		conversation.ID,
		message.Role,
		message.Content,
		now,
		message.Status,
		responseID,
		mustJSON(userMessageMetadata),
	); err != nil {
		return store.Conversation{}, store.Message{}, err
	}

	for _, asset := range input.PreparedImages {
		assetID := "asset_" + uuid.NewString()
		refID := "assetref_" + uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO media_assets(
				id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		`,
			assetID,
			strings.TrimSpace(input.OwnerID),
			strings.TrimSpace(asset.FileID),
			strings.TrimSpace(asset.Path),
			strings.TrimSpace(asset.MediaType),
			asset.Bytes,
			strings.TrimSpace(asset.SHA256),
			asset.Width,
			asset.Height,
			strings.TrimSpace(asset.SourceKind),
			strings.TrimSpace(asset.OriginalName),
			strings.TrimSpace(asset.OriginalMediaType),
			now,
		); err != nil {
			return store.Conversation{}, store.Message{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO media_asset_refs(
				id, asset_id, file_id, owner_id, request_id, conversation_id, message_id, input_part_index, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`,
			refID,
			assetID,
			strings.TrimSpace(asset.FileID),
			strings.TrimSpace(input.OwnerID),
			strings.TrimSpace(input.RequestID),
			conversation.ID,
			message.ID,
			asset.InputPartIndex,
			now,
		); err != nil {
			return store.Conversation{}, store.Message{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		s.logger(ctx).Warn("postgresql create pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	s.logger(ctx).Debug("postgresql pending turn created", zap.String("conversation.id", input.ConversationID), zap.String("request.id", input.RequestID), zap.String("owner.id", input.OwnerID))
	return conversation, message, nil
}

func (s *Store) UpdateDraft(ctx context.Context, input store.UpdateDraftInput) (store.Conversation, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isDraftWritable(metadata) {
		return store.Conversation{}, store.ErrTurnConflict
	}
	metadata["realtime_draft_text"] = input.DraftText
	metadata["realtime_status"] = "streaming"
	conversation.Metadata = metadata
	conversation.UpdatedAt = time.Now().UTC()

	if _, err := s.pool.Exec(ctx, `
		UPDATE conversations
		SET updated_at = $1, metadata_json = $2::jsonb
		WHERE id = $3
	`, conversation.UpdatedAt, mustJSON(metadata), conversation.ID); err != nil {
		s.logger(ctx).Warn("postgresql update draft failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, err
	}
	s.logger(ctx).Debug("postgresql draft updated", zap.String("conversation.id", input.ConversationID), zap.Int("draft.length", len([]rune(input.DraftText))))
	return conversation, nil
}

func (s *Store) CompletePendingTurn(ctx context.Context, input store.CompletePendingInput) (store.Conversation, store.Message, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isTurnCompletable(metadata) {
		return store.Conversation{}, store.Message{}, store.ErrTurnConflict
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
	responseID := strings.TrimSpace(input.ResponseID)
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.logger(ctx).Warn("postgresql complete pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
	`, message.ID, conversation.ID, message.Role, message.Content, now, message.Status, responseID, mustJSON(messageMetadata)); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE conversations
		SET updated_at = $1, last_message_at = $2, message_count = $3, last_message_preview = $4, metadata_json = $5::jsonb
		WHERE id = $6
	`, now, now, conversation.MessageCount, conversation.LastMessagePreview, mustJSON(metadata), conversation.ID); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger(ctx).Warn("postgresql complete pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	s.logger(ctx).Debug("postgresql pending turn completed", zap.String("conversation.id", input.ConversationID), zap.String("response.id", input.ResponseID), zap.String("mode", input.Mode))
	return conversation, message, nil
}

func (s *Store) AbortPendingTurn(ctx context.Context, input store.AbortPendingInput) (store.Conversation, store.Message, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isTurnCompletable(metadata) {
		return store.Conversation{}, store.Message{}, store.ErrTurnConflict
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.logger(ctx).Warn("postgresql abort pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
	`, message.ID, conversation.ID, message.Role, message.Content, now, message.Status, nil, mustJSON(message.Metadata)); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE conversations
		SET updated_at = $1, last_message_at = $2, message_count = $3, last_message_preview = $4, metadata_json = $5::jsonb
		WHERE id = $6
	`, now, now, conversation.MessageCount, conversation.LastMessagePreview, mustJSON(metadata), conversation.ID); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger(ctx).Warn("postgresql abort pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return store.Conversation{}, store.Message{}, err
	}
	s.logger(ctx).Debug("postgresql pending turn aborted", zap.String("conversation.id", input.ConversationID))
	return conversation, message, nil
}

func scanConversation(row rowScanner) (store.Conversation, error) {
	var item store.Conversation
	var metadataJSON []byte
	if err := row.Scan(
		&item.ID,
		&item.Title,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.LastMessageAt,
		&item.MessageCount,
		&item.LastMessagePreview,
		&item.LastUserText,
		&metadataJSON,
		&item.ResponseID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Conversation{}, store.ErrNotFound
		}
		return store.Conversation{}, err
	}
	item.Metadata = parseJSONMap(metadataJSON)
	return item, nil
}

func scanRequestRow(scanner rowScanner) (store.Request, error) {
	var item store.Request
	var messageMetadataJSON []byte
	var conversationMetadataJSON []byte
	if err := scanner.Scan(
		&item.ConversationID,
		&item.CreatedAt,
		&messageMetadataJSON,
		&item.UpdatedAt,
		&conversationMetadataJSON,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Request{}, store.ErrNotFound
		}
		return store.Request{}, err
	}

	messageMetadata := parseJSONMap(messageMetadataJSON)
	requestDebug, _ := messageMetadata["request_debug"].(map[string]any)
	conversationMetadata := parseJSONMap(conversationMetadataJSON)

	item.RequestID = metadataString(requestDebug, "request_id", "")
	item.OwnerID = metadataString(conversationMetadata, "owner_id", "")
	item.ResponseID = metadataString(requestDebug, "response_id", "")
	item.RequestFormat = metadataString(requestDebug, "request_format", "")
	item.Model = metadataString(requestDebug, "model", "")
	item.InputText = metadataString(requestDebug, "input_text", "")
	item.RequestMethod = metadataString(requestDebug, "request_method", "")
	item.RequestPath = metadataString(requestDebug, "request_path", "")
	item.RequestQuery = parseStringSliceMap(requestDebug["request_query"])
	item.RequestHeaders = parseStringSliceMap(requestDebug["request_headers"])
	item.Status = metadataString(conversationMetadata, "realtime_status", "")
	item.Metadata = messageMetadata
	item.RequestBody, _ = requestDebug["request_body"].(map[string]any)
	item.ToolSchemas, _ = requestDebug["tool_schemas"].([]any)
	item.InputParts = parseRequestInputParts(requestDebug["input_parts"])
	item.ToolChoice = parseRequestToolChoice(requestDebug["tool_choice"])
	item.ResponseFormat = parseRequestResponseFormat(requestDebug["response_format"])
	item.SystemText = metadataString(requestDebug, "system_text", "")
	item.DeveloperText = metadataString(requestDebug, "developer_text", "")
	item.AssistantText = metadataString(requestDebug, "assistant_text", "")
	return item, nil
}

func parseRequestInputParts(value any) []store.RequestInputPart {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	parts := make([]store.RequestInputPart, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		parts = append(parts, store.RequestInputPart{
			Type:      metadataString(record, "type", ""),
			Text:      metadataString(record, "text", ""),
			MediaType: metadataString(record, "media_type", ""),
			URL:       metadataString(record, "url", ""),
		})
	}
	return parts
}

func parseStringSliceMap(value any) map[string][]string {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string][]string, len(record))
	for key, raw := range record {
		items, ok := raw.([]any)
		if !ok {
			continue
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				continue
			}
			values = append(values, text)
		}
		if len(values) == 0 {
			continue
		}
		result[key] = values
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseRequestToolChoice(value any) store.RequestToolChoice {
	record, _ := value.(map[string]any)
	return store.RequestToolChoice{
		Type: metadataString(record, "type", ""),
		Name: metadataString(record, "name", ""),
	}
}

func parseRequestResponseFormat(value any) store.RequestResponseFormat {
	record, _ := value.(map[string]any)
	format := store.RequestResponseFormat{
		Type: metadataString(record, "type", ""),
		Name: metadataString(record, "name", ""),
	}
	format.Schema, _ = record["schema"].(map[string]any)
	return format
}

func scanMessage(row rowScanner) (store.Message, error) {
	var item store.Message
	var status *string
	var responseID *string
	var metadataJSON []byte
	if err := row.Scan(&item.ID, &item.Role, &item.Content, &item.CreatedAt, &status, &responseID, &metadataJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Message{}, store.ErrNotFound
		}
		return store.Message{}, err
	}
	if status != nil {
		item.Status = *status
	}
	item.ResponseID = responseID
	item.Metadata = parseJSONMap(metadataJSON)
	return item, nil
}

func keysOf(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func buildConversationTitle(userContent string) string {
	text := strings.TrimSpace(userContent)
	if text == "" {
		return "新会话"
	}
	runes := []rune(text)
	if len(runes) > 24 {
		return string(runes[:24])
	}
	return text
}

func stringValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func isDraftWritable(metadata map[string]any) bool {
	status := metadataString(metadata, "realtime_status", "waiting")
	return status == "waiting" || status == "streaming"
}

func isTurnCompletable(metadata map[string]any) bool {
	status := metadataString(metadata, "realtime_status", "waiting")
	return status == "waiting" || status == "streaming"
}

func metadataString(metadata map[string]any, key string, fallback string) string {
	value, _ := metadata[key].(string)
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
