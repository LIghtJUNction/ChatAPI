package sqlite

import (
	"context"
	"database/sql"
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
		SELECT id, title, created_at, updated_at
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
		var createdAt string
		var updatedAt string
		if err := rows.Scan(&item.ID, &item.Title, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		item.LastMessageAt = item.UpdatedAt
		items = append(items, item)
	}
	return items, rows.Err()
}

func parseTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339, raw)
	if err == nil {
		return t
	}
	return time.Time{}
}
