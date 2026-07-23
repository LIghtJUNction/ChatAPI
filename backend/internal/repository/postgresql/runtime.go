package postgresql

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrationplan"
)

type Store struct {
	pool   *pgxpool.Pool
	Logger *zap.Logger
}

var errConflict = common.ErrTurnConflict

const BootstrapVersion = "0001_bootstrap"
const LatestVersion = "0009_postgresql_virtual_models"

//go:embed sql/*.up.sql
var migrationFiles embed.FS

var registeredMigrations = mustLoadMigrations()
var bootstrapSchema = mustBootstrapSchema(registeredMigrations)

func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgresql pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgresql: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) logger(ctx context.Context) *zap.Logger {
	return logging.BindContext(s.Logger, ctx)
}

func Bootstrap(ctx context.Context, pool *pgxpool.Pool) error {
	lockConnection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire postgresql migration lock connection: %w", err)
	}
	defer lockConnection.Release()
	if _, err := lockConnection.Exec(ctx, `SELECT pg_advisory_lock(hashtext('chatapi_schema_migrations'))`); err != nil {
		return fmt.Errorf("lock postgresql migrations: %w", err)
	}
	defer func() {
		_, _ = lockConnection.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtext('chatapi_schema_migrations'))`)
	}()

	_, err = pool.Exec(ctx, bootstrapSchema)
	if err != nil {
		return fmt.Errorf("bootstrap postgresql schema: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO db_meta(key, value, updated_at)
		VALUES
			('schema_version', '0001_bootstrap', NOW()),
			('migration_dirty', '0', NOW()),
			('migration_lock', '', NOW()),
			('created_by', 'go', NOW()),
			('last_migrated_at', TO_CHAR(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'), NOW())
		ON CONFLICT(key) DO NOTHING;

		INSERT INTO schema_migrations(version, name, applied_at, checksum, dirty)
		VALUES ('0001_bootstrap', 'bootstrap', NOW(), '', false)
		ON CONFLICT(version) DO NOTHING;
	`); err != nil {
		return fmt.Errorf("seed postgresql migration metadata: %w", err)
	}
	statusStore := &Store{pool: pool}
	status, err := statusStore.MigrationStatus(ctx)
	if err != nil {
		return err
	}
	if status.MigrationDirty {
		return nil
	}
	return applyPendingMigrations(ctx, pool, status)
}

func Reset(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS user_configs;
		DROP TABLE IF EXISTS auth_verification_codes;
		DROP TABLE IF EXISTS config;
		DROP TABLE IF EXISTS audit_logs;
		DROP TABLE IF EXISTS automation_rules;
		DROP TABLE IF EXISTS app_api_key_audit_logs;
		DROP TABLE IF EXISTS user_app_api_keys;
		DROP TABLE IF EXISTS user_api_keys;
		DROP TABLE IF EXISTS user_virtual_models;
		DROP TABLE IF EXISTS uploaded_images;
		DROP TABLE IF EXISTS storage_user_quotas;
		DROP TABLE IF EXISTS storage_file_deletion_failures;
		DROP TABLE IF EXISTS media_asset_event_refs;
		DROP TABLE IF EXISTS media_asset_staging;
		DROP TABLE IF EXISTS media_asset_refs;
		DROP TABLE IF EXISTS media_assets;
		DROP TABLE IF EXISTS conversation_events;
		DROP TABLE IF EXISTS messages;
		DROP TABLE IF EXISTS conversations;
		DROP TABLE IF EXISTS schema_migrations;
		DROP TABLE IF EXISTS db_meta;
		DROP TABLE IF EXISTS user_identities;
		DROP TABLE IF EXISTS users;
	`)
	if err != nil {
		return fmt.Errorf("reset postgresql schema: %w", err)
	}
	return nil
}

func applyPendingMigrations(ctx context.Context, pool *pgxpool.Pool, status common.MigrationStatus) error {
	applied := make(map[string]common.AppliedMigration, len(status.Applied))
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
		if err := applyMigration(ctx, pool, migration); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, migration migrationplan.Step) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, migration.UpSQL); err != nil {
		return fmt.Errorf("apply postgresql migration %s: %w", migration.Version, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO schema_migrations(version, name, applied_at, checksum, dirty)
		VALUES ($1, $2, NOW(), '', false)
	`, migration.Version, migration.Name); err != nil {
		return fmt.Errorf("record postgresql migration %s: %w", migration.Version, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO db_meta(key, value, updated_at)
		VALUES ('schema_version', $1, NOW())
		ON CONFLICT(key) DO UPDATE SET
			value = EXCLUDED.value,
			updated_at = EXCLUDED.updated_at
	`, migration.Version); err != nil {
		return fmt.Errorf("update postgresql schema_version %s: %w", migration.Version, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO db_meta(key, value, updated_at)
		VALUES ('last_migrated_at', TO_CHAR(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'), NOW())
		ON CONFLICT(key) DO UPDATE SET
			value = EXCLUDED.value,
			updated_at = EXCLUDED.updated_at
	`); err != nil {
		return fmt.Errorf("update postgresql last_migrated_at %s: %w", migration.Version, err)
	}
	return tx.Commit(ctx)
}

func init() {
	if len(registeredMigrations) == 0 {
		panic("postgresql migrations not loaded")
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
	panic("postgresql bootstrap migration not found")
}
