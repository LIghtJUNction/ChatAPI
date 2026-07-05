package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json
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
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json
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
	return store.Conversation{}, store.Message{}, errors.New("CreatePendingTurn not implemented yet")
}

func (s *Store) UpdateDraft(ctx context.Context, input store.UpdateDraftInput) (store.Conversation, error) {
	return store.Conversation{}, errors.New("UpdateDraft not implemented yet")
}

func (s *Store) CompletePendingTurn(ctx context.Context, input store.CompletePendingInput) (store.Conversation, store.Message, error) {
	return store.Conversation{}, store.Message{}, errors.New("CompletePendingTurn not implemented yet")
}

func (s *Store) AbortPendingTurn(ctx context.Context, conversationID string, reason string) (store.Conversation, store.Message, error) {
	return store.Conversation{}, store.Message{}, errors.New("AbortPendingTurn not implemented yet")
}

func parseTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339, raw)
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
