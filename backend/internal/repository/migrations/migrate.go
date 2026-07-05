package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

const bootstrapSchema = `
CREATE TABLE IF NOT EXISTS db_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS conversations (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	last_message_at TEXT NOT NULL,
	message_count INTEGER NOT NULL DEFAULT 0,
	last_message_preview TEXT NOT NULL DEFAULT '',
	last_user_text TEXT NOT NULL DEFAULT '',
	metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	created_at TEXT NOT NULL,
	status TEXT,
	response_id TEXT,
	metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation_created_at
ON messages(conversation_id, created_at, id);

CREATE TABLE IF NOT EXISTS user_api_keys (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	name TEXT NOT NULL,
	key_ciphertext TEXT NOT NULL,
	key_prefix TEXT NOT NULL,
	model TEXT NOT NULL DEFAULT '',
	last_used_at TEXT,
	created_at TEXT NOT NULL,
	revoked_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_api_keys_key_prefix
ON user_api_keys(key_prefix);

CREATE INDEX IF NOT EXISTS idx_user_api_keys_user_id
ON user_api_keys(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS user_app_api_keys (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	name TEXT NOT NULL,
	key_hash TEXT NOT NULL,
	key_prefix TEXT NOT NULL,
	scopes_json TEXT NOT NULL DEFAULT '[]',
	resource_limits_json TEXT NOT NULL DEFAULT '{}',
	expires_at TEXT,
	last_used_at TEXT,
	created_at TEXT NOT NULL,
	revoked_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_app_api_keys_key_prefix
ON user_app_api_keys(key_prefix);

CREATE TABLE IF NOT EXISTS app_api_key_audit_logs (
	id TEXT PRIMARY KEY,
	app_api_key_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	route TEXT NOT NULL,
	status_code INTEGER NOT NULL,
	error_code TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
`

func Bootstrap(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, bootstrapSchema); err != nil {
		return fmt.Errorf("bootstrap schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO db_meta(key, value)
		VALUES ('schema_version', '0001_bootstrap')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`); err != nil {
		return fmt.Errorf("seed db_meta: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO schema_migrations(version)
		VALUES ('0001_bootstrap')
		ON CONFLICT(version) DO NOTHING
	`); err != nil {
		return fmt.Errorf("seed schema_migrations: %w", err)
	}
	return nil
}
