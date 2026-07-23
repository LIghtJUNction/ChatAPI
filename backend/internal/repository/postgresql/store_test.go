package postgresql

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	platformrepo "github.com/zyf2007/ChatAPI/internal/repository/platform"
	"github.com/zyf2007/ChatAPI/internal/repository/repositorycontract"
	"github.com/zyf2007/ChatAPI/internal/repository/storetest"
	"github.com/zyf2007/ChatAPI/internal/testutil/pgtest"
)

func TestPostgreSQLRejectsSQLiteMaintenanceOperations(t *testing.T) {
	store := openTestStore()(t)
	if err := store.Checkpoint(context.Background()); !errors.Is(err, platformrepo.ErrMaintenanceUnsupported) {
		t.Fatalf("checkpoint error = %v", err)
	}
	if err := store.Vacuum(context.Background()); !errors.Is(err, platformrepo.ErrMaintenanceUnsupported) {
		t.Fatalf("vacuum error = %v", err)
	}
}

func TestOutputMediaMigrationKeepsURLOnEventRefs(t *testing.T) {
	var migrationSQL string
	for _, step := range registeredMigrations {
		if step.Version == "0006_postgresql_output_media_refs" {
			migrationSQL = step.UpSQL
			break
		}
	}
	inputRefs := tableDefinition(t, migrationSQL, "media_asset_refs")
	eventRefs := tableDefinition(t, migrationSQL, "media_asset_event_refs")
	if strings.Contains(inputRefs, "url TEXT") {
		t.Fatalf("input media refs unexpectedly require output URL: %s", inputRefs)
	}
	if !strings.Contains(eventRefs, "url TEXT NOT NULL") {
		t.Fatalf("event media refs do not persist output URL: %s", eventRefs)
	}
}

func tableDefinition(t *testing.T, migrationSQL string, table string) string {
	t.Helper()
	start := strings.Index(migrationSQL, "CREATE TABLE IF NOT EXISTS "+table+" (")
	if start < 0 {
		t.Fatalf("missing table %s", table)
	}
	rest := migrationSQL[start:]
	end := strings.Index(rest, ");")
	if end < 0 {
		t.Fatalf("unterminated table %s", table)
	}
	return rest[:end+2]
}

func TestPostgreSQLRepositoryContracts(t *testing.T) {
	pgtest.BaseDSN(t)
	storetest.RunUserRepositoryTests(t, openTestStore())
	storetest.RunConfigRepositoryTests(t, openTestStore())
	storetest.RunAuthRepositoryTests(t, openTestStore())
	storetest.RunAPIKeyRepositoryTests(t, openTestStore())
	storetest.RunAuditRepositoryTests(t, openTestStore())
	storetest.RunAutomationRepositoryTests(t, openTestStore())
	storetest.RunStorageRepositoryTests(t, openTestStore())
	storetest.RunConversationRepositoryTests(t, openTestStore())
}

func TestBootstrapAppliesLatestPostgreSQLMigration(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
	ctx := context.Background()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgresql store: %v", err)
	}
	t.Cleanup(st.Close)
	if err := Reset(ctx, st.Pool()); err != nil {
		t.Fatalf("reset postgresql test schema: %v", err)
	}
	if err := Bootstrap(ctx, st.Pool()); err != nil {
		t.Fatalf("bootstrap postgresql schema: %v", err)
	}
	status, err := st.MigrationStatus(ctx)
	if err != nil {
		t.Fatalf("migration status: %v", err)
	}
	if status.SchemaVersion != LatestVersion || len(status.Applied) != len(registeredMigrations) {
		t.Fatalf("unexpected postgresql migration status: %#v", status)
	}
	for _, index := range []string{"idx_messages_response_id_nonempty", "idx_messages_request_debug_request_id"} {
		var exists bool
		if err := st.Pool().QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM pg_indexes
				WHERE schemaname = current_schema()
					AND indexname = $1
			)
		`, index).Scan(&exists); err != nil {
			t.Fatalf("inspect index %s: %v", index, err)
		}
		if !exists {
			t.Fatalf("expected postgresql migration index %s", index)
		}
	}
}

func TestBootstrapRejectsDirtyPostgreSQLMigration(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
	ctx := context.Background()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := Bootstrap(ctx, store.Pool()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool().Exec(ctx, `UPDATE db_meta SET value='1' WHERE key='migration_dirty'`); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(ctx, store.Pool()); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty PostgreSQL bootstrap error=%v", err)
	}
}

func TestConcurrentBootstrapSerializesPostgreSQLMigrations(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
	ctx := context.Background()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := Reset(ctx, st.Pool()); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errorsFound := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errorsFound <- Bootstrap(ctx, st.Pool())
		}()
	}
	close(start)
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("concurrent bootstrap failed: %v", err)
		}
	}
}

func TestBootstrapDoesNotConsumeTheOnlyPoolConnectionForMigrationLock(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := Reset(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(ctx, pool); err != nil {
		t.Fatalf("bootstrap with one pool connection: %v", err)
	}
}

func TestVirtualModelMigrationGroupsExistingAPIKeys(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
	ctx := context.Background()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgresql store: %v", err)
	}
	t.Cleanup(st.Close)
	if _, err := st.Pool().Exec(ctx, `
		CREATE TABLE user_api_keys(user_id TEXT NOT NULL, model TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL);
		INSERT INTO user_api_keys(user_id, model, created_at) VALUES
			('u1', ' model-a ', NOW()),
			('u1', 'model-a', NOW()),
			('u1', 'model-b', NOW());
	`); err != nil {
		t.Fatalf("seed legacy api keys: %v", err)
	}
	var migrationSQL string
	for _, step := range registeredMigrations {
		if step.Version == LatestVersion {
			migrationSQL = step.UpSQL
			break
		}
	}
	if _, err := st.Pool().Exec(ctx, migrationSQL); err != nil {
		t.Fatalf("apply virtual model migration: %v", err)
	}
	var count int
	if err := st.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM user_virtual_models WHERE user_id='u1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("virtual model count = %d, want 2", count)
	}
}

func openTestStore() storetest.NewStoreFunc {
	return func(t *testing.T) repositorycontract.Store {
		t.Helper()
		ctx := context.Background()
		dsn := pgtest.IsolatedDSN(t)
		st, err := Open(ctx, dsn)
		if err != nil {
			t.Fatalf("open postgresql store: %v", err)
		}
		t.Cleanup(st.Close)
		if err := resetTestSchema(ctx, st); err != nil {
			t.Fatalf("reset postgresql test schema: %v", err)
		}
		if err := Bootstrap(ctx, st.Pool()); err != nil {
			t.Fatalf("bootstrap postgresql schema: %v", err)
		}
		return st
	}
}

func resetTestSchema(ctx context.Context, st *Store) error {
	return Reset(ctx, st.Pool())
}
