package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	_ "modernc.org/sqlite"

	"github.com/zyf/chatapi/internal/store"
)

type Store struct {
	db *sql.DB
}

var errNotFound = errors.New("record not found")

func Open(dsn string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

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
			return store.Conversation{}, errNotFound
		}
		return store.Conversation{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	item.LastMessageAt = parseTime(lastMessageAt)
	item.Metadata = parseJSONMap(metadataJSON)
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
	return items, rows.Err()
}

func (s *Store) CreatePendingTurn(ctx context.Context, input store.CreatePendingInput) (store.Conversation, store.Message, error) {
	now := time.Now().UTC()
	metadata := map[string]any{
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
			"request_id":     input.RequestID,
			"response_id":    input.ResponseID,
			"model":          input.Model,
			"request_format": input.RequestFormat,
			"request_keys":   keysOf(input.RequestBody),
			"input_text":     input.UserContent,
			"request_body":   input.RequestBody,
			"tool_schemas":   input.ToolSchemas,
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

	if err := tx.Commit(); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	return conversation, message, nil
}

func (s *Store) UpdateDraft(ctx context.Context, input store.UpdateDraftInput) (store.Conversation, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	metadata["realtime_draft_text"] = input.DraftText
	conversation.Metadata = metadata
	conversation.UpdatedAt = time.Now().UTC()

	if _, err := s.db.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(conversation.UpdatedAt), mustJSON(metadata), conversation.ID); err != nil {
		return store.Conversation{}, err
	}
	return conversation, nil
}

func (s *Store) CompletePendingTurn(ctx context.Context, input store.CompletePendingInput) (store.Conversation, store.Message, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	metadata := ensureMap(conversation.Metadata)
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
		return store.Conversation{}, store.Message{}, err
	}
	return conversation, message, nil
}

func (s *Store) AbortPendingTurn(ctx context.Context, input store.AbortPendingInput) (store.Conversation, store.Message, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	metadata := ensureMap(conversation.Metadata)
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
		return store.Conversation{}, store.Message{}, err
	}
	return conversation, message, nil
}

func parseTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return t
	}
	t, err = time.Parse(time.RFC3339, raw)
	if err == nil {
		return t
	}
	return time.Time{}
}

func parseJSONMap(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil
	}
	return value
}

func ensureMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
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
