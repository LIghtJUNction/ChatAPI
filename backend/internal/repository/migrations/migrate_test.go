package migrations

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBootstrapSeedsMigrationMetadata(t *testing.T) {
	db := openTestDB(t)

	if err := Bootstrap(context.Background(), db); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	status, err := StatusReport(context.Background(), db)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.SchemaVersion != BootstrapVersion {
		t.Fatalf("unexpected schema version: %#v", status)
	}
	if status.MigrationDirty {
		t.Fatalf("new bootstrap should not be dirty: %#v", status)
	}
	if status.CreatedBy != "go" || status.LastMigratedAt == "" {
		t.Fatalf("missing migration meta: %#v", status)
	}
	if len(status.Applied) != 1 || status.Applied[0].Version != BootstrapVersion || status.Applied[0].Name != "bootstrap" {
		t.Fatalf("unexpected applied migrations: %#v", status.Applied)
	}
	for _, table := range []string{"users", "user_identities", "user_configs", "config"} {
		if !tableExists(t, db, table) {
			t.Fatalf("expected bootstrap table %s", table)
		}
	}
	for _, index := range []string{"idx_users_username", "idx_users_email", "idx_user_identities_user_provider"} {
		if !indexExists(t, db, index) {
			t.Fatalf("expected bootstrap index %s", index)
		}
	}
}

func TestBootstrapUpgradesThinMetadataTables(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE db_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
		INSERT INTO schema_migrations(version, applied_at) VALUES ('legacy', '2026-01-01T00:00:00Z');
	`); err != nil {
		t.Fatalf("seed legacy metadata: %v", err)
	}

	if err := Bootstrap(context.Background(), db); err != nil {
		t.Fatalf("bootstrap legacy: %v", err)
	}
	status, err := StatusReport(context.Background(), db)
	if err != nil {
		t.Fatalf("status legacy: %v", err)
	}
	if status.SchemaVersion != BootstrapVersion || status.Meta["migration_dirty"] != "0" {
		t.Fatalf("unexpected upgraded meta: %#v", status)
	}
	if len(status.Applied) != 2 {
		t.Fatalf("expected legacy and bootstrap migrations: %#v", status.Applied)
	}
}

func TestStatusReportDetectsDirtyMigration(t *testing.T) {
	db := openTestDB(t)
	if err := Bootstrap(context.Background(), db); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE schema_migrations SET dirty = 1 WHERE version = ?`, BootstrapVersion); err != nil {
		t.Fatalf("mark dirty: %v", err)
	}

	status, err := StatusReport(context.Background(), db)
	if err != nil {
		t.Fatalf("status dirty: %v", err)
	}
	if !status.MigrationDirty {
		t.Fatalf("expected dirty status: %#v", status)
	}
}

func TestBootstrapDoesNotClearDirtyMetadata(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE db_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
		INSERT INTO db_meta(key, value) VALUES ('schema_version', '0002_next');
		INSERT INTO db_meta(key, value) VALUES ('migration_dirty', '1');
	`); err != nil {
		t.Fatalf("seed dirty metadata: %v", err)
	}

	if err := Bootstrap(context.Background(), db); err != nil {
		t.Fatalf("bootstrap dirty metadata: %v", err)
	}
	status, err := StatusReport(context.Background(), db)
	if err != nil {
		t.Fatalf("status dirty metadata: %v", err)
	}
	if status.SchemaVersion != "0002_next" || !status.MigrationDirty {
		t.Fatalf("bootstrap should not downgrade version or clear dirty: %#v", status)
	}
}

func TestResetDropsBootstrapSchema(t *testing.T) {
	db := openTestDB(t)
	if err := Bootstrap(context.Background(), db); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if err := Reset(context.Background(), db); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := StatusReport(context.Background(), db); err == nil {
		t.Fatal("expected status after reset to fail")
	}
	for _, table := range bootstrapTables {
		var count int
		if err := db.QueryRowContext(context.Background(), `
			SELECT COUNT(*)
			FROM sqlite_master
			WHERE type = 'table' AND name = ?
		`, table).Scan(&count); err != nil {
			t.Fatalf("inspect table %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected table %s to be dropped", table)
		}
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, table).Scan(&count); err != nil {
		t.Fatalf("inspect table %s: %v", table, err)
	}
	return count == 1
}

func indexExists(t *testing.T, db *sql.DB, index string) bool {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'index' AND name = ?
	`, index).Scan(&count); err != nil {
		t.Fatalf("inspect index %s: %v", index, err)
	}
	return count == 1
}
