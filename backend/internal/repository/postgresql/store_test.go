package postgresql

import (
	"context"
	"os"
	"testing"

	"github.com/zyf/chatapi/internal/repository/storetest"
	"github.com/zyf/chatapi/internal/store"
)

func TestPostgreSQLRepositoryContracts(t *testing.T) {
	dsn := os.Getenv("CHATAPI_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("CHATAPI_PG_TEST_DSN is not set")
	}
	storetest.RunUserRepositoryTests(t, openTestStore(dsn))
	storetest.RunConfigRepositoryTests(t, openTestStore(dsn))
	storetest.RunAPIKeyRepositoryTests(t, openTestStore(dsn))
	storetest.RunAuditRepositoryTests(t, openTestStore(dsn))
	storetest.RunAutomationRepositoryTests(t, openTestStore(dsn))
	storetest.RunStorageRepositoryTests(t, openTestStore(dsn))
}

func openTestStore(dsn string) storetest.NewStoreFunc {
	return func(t *testing.T) store.Store {
		t.Helper()
		ctx := context.Background()
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
	_, err := st.Pool().Exec(ctx, `
		DROP TABLE IF EXISTS user_configs;
		DROP TABLE IF EXISTS config;
		DROP TABLE IF EXISTS audit_logs;
		DROP TABLE IF EXISTS automation_rules;
		DROP TABLE IF EXISTS app_api_key_audit_logs;
		DROP TABLE IF EXISTS user_app_api_keys;
		DROP TABLE IF EXISTS user_api_keys;
		DROP TABLE IF EXISTS uploaded_images;
		DROP TABLE IF EXISTS storage_user_quotas;
		DROP TABLE IF EXISTS storage_file_deletion_failures;
		DROP TABLE IF EXISTS user_identities;
		DROP TABLE IF EXISTS users;
	`)
	return err
}
