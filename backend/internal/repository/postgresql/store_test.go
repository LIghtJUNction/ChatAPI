package postgresql

import (
	"context"
	"testing"

	"github.com/zyf/chatapi/internal/repository/storetest"
	"github.com/zyf/chatapi/internal/store"
	"github.com/zyf/chatapi/internal/testutil/pgtest"
)

func TestPostgreSQLRepositoryContracts(t *testing.T) {
	pgtest.BaseDSN(t)
	storetest.RunUserRepositoryTests(t, openTestStore())
	storetest.RunConfigRepositoryTests(t, openTestStore())
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
	if status.SchemaVersion != LatestVersion || len(status.Applied) != 2 {
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
	return func(t *testing.T) store.Store {
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
