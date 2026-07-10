package postgresql

import (
	"context"
	"strings"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/repository/repositorycontract"
	"github.com/zyf2007/ChatAPI/internal/repository/storetest"
	"github.com/zyf2007/ChatAPI/internal/testutil/pgtest"
)

func TestOutputMediaMigrationKeepsURLOnEventRefs(t *testing.T) {
	var migrationSQL string
	for _, step := range registeredMigrations {
		if step.Version == LatestVersion {
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
