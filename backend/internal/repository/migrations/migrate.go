package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/zyf/chatapi/internal/repository/migrationplan"
)

const BootstrapVersion = "0001_bootstrap"
const LatestVersion = "0005_media_assets"

//go:embed sql/*.up.sql
var migrationFiles embed.FS

var registeredMigrations = mustLoadMigrations()
var bootstrapSchema = mustBootstrapSchema(registeredMigrations)

var bootstrapTables = []string{
	"storage_file_deletion_failures",
	"storage_user_quotas",
	"media_asset_refs",
	"media_assets",
	"uploaded_images",
	"audit_logs",
	"app_api_key_audit_logs",
	"user_app_api_keys",
	"user_identities",
	"user_configs",
	"auth_verification_codes",
	"automation_rules",
	"user_api_keys",
	"users",
	"config",
	"messages",
	"conversations",
	"schema_migrations",
	"db_meta",
}

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
	status, err := StatusReport(ctx, db)
	if err != nil {
		return err
	}
	if status.MigrationDirty {
		return nil
	}
	return applyPendingMigrations(ctx, db, status)
}

func Reset(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=OFF;`); err != nil {
		return fmt.Errorf("disable foreign keys: %w", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `PRAGMA foreign_keys=ON;`)
	}()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range bootstrapTables {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			return fmt.Errorf("drop table %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
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
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("seed db_meta last_migrated_at: %w", err)
	}
	return nil
}

func applyPendingMigrations(ctx context.Context, db *sql.DB, status Status) error {
	applied := make(map[string]AppliedMigration, len(status.Applied))
	for _, item := range status.Applied {
		applied[item.Version] = item
	}
	for _, migration := range registeredMigrations {
		if migration.Version == BootstrapVersion {
			continue
		}
		if _, ok := applied[migration.Version]; ok {
			continue
		}
		if err := applyMigration(ctx, db, migration); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, migration migrationplan.Step) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, migration.UpSQL); err != nil {
		return fmt.Errorf("apply migration %s: %w", migration.Version, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO schema_migrations(version, name, applied_at, checksum, dirty)
		VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), '', 0)
	`, migration.Version, migration.Name); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.Version, err)
	}
	if err := upsertMetaValue(ctx, tx, "schema_version", migration.Version); err != nil {
		return fmt.Errorf("update schema version %s: %w", migration.Version, err)
	}
	if err := upsertMetaValue(ctx, tx, "last_migrated_at", "now"); err != nil {
		return fmt.Errorf("update last_migrated_at %s: %w", migration.Version, err)
	}
	return tx.Commit()
}

func upsertMetaValue(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, key string, value string) error {
	var statement string
	var args []any
	if key == "last_migrated_at" && value == "now" {
		statement = `
			INSERT INTO db_meta(key, value, updated_at)
			VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			ON CONFLICT(key) DO UPDATE SET
				value = excluded.value,
				updated_at = excluded.updated_at
		`
		args = []any{key}
	} else {
		statement = `
			INSERT INTO db_meta(key, value, updated_at)
			VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			ON CONFLICT(key) DO UPDATE SET
				value = excluded.value,
				updated_at = excluded.updated_at
		`
		args = []any{key, value}
	}
	_, err := db.ExecContext(ctx, statement, args...)
	return err
}

func init() {
	if len(registeredMigrations) == 0 {
		panic("sqlite migrations not loaded")
	}
}

func mustLoadMigrations() []migrationplan.Step {
	steps, err := migrationplan.LoadSteps(migrationFiles, "sql")
	if err != nil {
		panic(err)
	}
	return steps
}

func mustBootstrapSchema(steps []migrationplan.Step) string {
	for _, step := range steps {
		if step.Version == BootstrapVersion {
			return step.UpSQL
		}
	}
	panic("sqlite bootstrap migration not found")
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
