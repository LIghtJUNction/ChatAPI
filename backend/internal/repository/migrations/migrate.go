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
	updated_at TEXT NOT NULL
);
`

func Bootstrap(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, bootstrapSchema); err != nil {
		return fmt.Errorf("bootstrap schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO db_meta(key, value)
		VALUES ('schema_version', '0001_bootstrap')
		ON CONFLICT(key) DO NOTHING
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
