package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

const BootstrapVersion = "0001_bootstrap"

const bootstrapSchema = `
CREATE TABLE IF NOT EXISTS db_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	checksum TEXT NOT NULL DEFAULT '',
	dirty INTEGER NOT NULL DEFAULT 0
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

CREATE TABLE IF NOT EXISTS automation_rules (
	id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	rule_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY(user_id, id)
);

CREATE INDEX IF NOT EXISTS idx_automation_rules_user_updated
ON automation_rules(user_id, updated_at DESC, id);

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

CREATE TABLE IF NOT EXISTS audit_logs (
	id TEXT PRIMARY KEY,
	actor_user_id TEXT NOT NULL DEFAULT '',
	actor_role TEXT NOT NULL DEFAULT '',
	actor_source TEXT NOT NULL DEFAULT '',
	event_type TEXT NOT NULL,
	resource_type TEXT NOT NULL DEFAULT '',
	resource_id TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL,
	outcome TEXT NOT NULL,
	ip_address TEXT NOT NULL DEFAULT '',
	user_agent TEXT NOT NULL DEFAULT '',
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created
ON audit_logs(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_created
ON audit_logs(actor_user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audit_logs_event_created
ON audit_logs(event_type, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS uploaded_images (
	id TEXT PRIMARY KEY,
	owner_id TEXT NOT NULL,
	filename TEXT NOT NULL,
	original_filename TEXT NOT NULL DEFAULT '',
	content_type TEXT NOT NULL,
	bytes INTEGER NOT NULL,
	url TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_uploaded_images_filename
ON uploaded_images(filename);

CREATE INDEX IF NOT EXISTS idx_uploaded_images_owner_created
ON uploaded_images(owner_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS storage_user_quotas (
	owner_id TEXT PRIMARY KEY,
	quota_bytes INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS storage_file_deletion_failures (
	path TEXT PRIMARY KEY,
	filename TEXT NOT NULL DEFAULT '',
	owner_id TEXT NOT NULL DEFAULT '',
	bytes INTEGER NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	attempts INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_storage_file_deletion_failures_updated
ON storage_file_deletion_failures(updated_at ASC, path ASC);
`

type Status struct {
	SchemaVersion  string             `json:"schema_version"`
	AppVersion     string             `json:"app_version,omitempty"`
	MigrationDirty bool               `json:"migration_dirty"`
	MigrationLock  string             `json:"migration_lock,omitempty"`
	CreatedBy      string             `json:"created_by,omitempty"`
	LastMigratedAt string             `json:"last_migrated_at,omitempty"`
	Meta           map[string]string  `json:"meta"`
	Applied        []AppliedMigration `json:"applied"`
}

type AppliedMigration struct {
	Version   string `json:"version"`
	Name      string `json:"name,omitempty"`
	AppliedAt string `json:"applied_at"`
	Checksum  string `json:"checksum,omitempty"`
	Dirty     bool   `json:"dirty"`
}

func Bootstrap(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, bootstrapSchema); err != nil {
		return fmt.Errorf("bootstrap schema: %w", err)
	}
	if err := ensureMetaColumns(ctx, db); err != nil {
		return err
	}
	if err := ensureMigrationColumns(ctx, db); err != nil {
		return err
	}
	meta := map[string]string{
		"schema_version":  BootstrapVersion,
		"migration_dirty": "0",
		"migration_lock":  "",
		"created_by":      "go",
	}
	for key, value := range meta {
		if err := insertMetaIfMissing(ctx, db, key, value); err != nil {
			return fmt.Errorf("seed db_meta %s: %w", key, err)
		}
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO schema_migrations(version, name, applied_at, checksum, dirty)
		VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), '', 0)
		ON CONFLICT(version) DO NOTHING
	`, BootstrapVersion, "bootstrap"); err != nil {
		return fmt.Errorf("seed schema_migrations: %w", err)
	}
	if err := setLastMigratedAt(ctx, db); err != nil {
		return err
	}
	return nil
}

func StatusReport(ctx context.Context, db *sql.DB) (Status, error) {
	meta, err := readMeta(ctx, db)
	if err != nil {
		return Status{}, err
	}
	applied, err := readAppliedMigrations(ctx, db)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		SchemaVersion:  meta["schema_version"],
		AppVersion:     meta["app_version"],
		MigrationDirty: meta["migration_dirty"] == "1",
		MigrationLock:  meta["migration_lock"],
		CreatedBy:      meta["created_by"],
		LastMigratedAt: meta["last_migrated_at"],
		Meta:           meta,
		Applied:        applied,
	}
	for _, item := range applied {
		if item.Dirty {
			status.MigrationDirty = true
			break
		}
	}
	return status, nil
}

func ensureMetaColumns(ctx context.Context, db *sql.DB) error {
	columns, err := tableColumns(ctx, db, "db_meta")
	if err != nil {
		return err
	}
	if !columns["updated_at"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE db_meta ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add db_meta.updated_at: %w", err)
		}
	}
	return nil
}

func ensureMigrationColumns(ctx context.Context, db *sql.DB) error {
	columns, err := tableColumns(ctx, db, "schema_migrations")
	if err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		sql  string
	}{
		{"name", `ALTER TABLE schema_migrations ADD COLUMN name TEXT NOT NULL DEFAULT ''`},
		{"checksum", `ALTER TABLE schema_migrations ADD COLUMN checksum TEXT NOT NULL DEFAULT ''`},
		{"dirty", `ALTER TABLE schema_migrations ADD COLUMN dirty INTEGER NOT NULL DEFAULT 0`},
	} {
		if columns[column.name] {
			continue
		}
		if _, err := db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("add schema_migrations.%s: %w", column.name, err)
		}
	}
	return nil
}

func insertMetaIfMissing(ctx context.Context, db *sql.DB, key string, value string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO db_meta(key, value, updated_at)
		VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		ON CONFLICT(key) DO NOTHING
	`, key, value)
	return err
}

func setLastMigratedAt(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO db_meta(key, value, updated_at)
		VALUES ('last_migrated_at', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		ON CONFLICT(key) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("seed db_meta last_migrated_at: %w", err)
	}
	return nil
}

func readMeta(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM db_meta`)
	if err != nil {
		return nil, fmt.Errorf("read db_meta: %w", err)
	}
	defer rows.Close()
	meta := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		meta[key] = value
	}
	return meta, rows.Err()
}

func readAppliedMigrations(ctx context.Context, db *sql.DB) ([]AppliedMigration, error) {
	rows, err := db.QueryContext(ctx, `SELECT version, name, applied_at, checksum, dirty FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	items := []AppliedMigration{}
	for rows.Next() {
		var item AppliedMigration
		var dirty int
		if err := rows.Scan(&item.Version, &item.Name, &item.AppliedAt, &item.Checksum, &dirty); err != nil {
			return nil, err
		}
		item.Dirty = dirty != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}
