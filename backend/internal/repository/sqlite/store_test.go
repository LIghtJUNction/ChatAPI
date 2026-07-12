package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	"github.com/zyf2007/ChatAPI/internal/repository/repositorycontract"
	"github.com/zyf2007/ChatAPI/internal/repository/storetest"
)

func TestSQLiteRepositoryContracts(t *testing.T) {
	storetest.RunUserRepositoryTests(t, openTestStore)
	storetest.RunConfigRepositoryTests(t, openTestStore)
	storetest.RunAuthRepositoryTests(t, openTestStore)
	storetest.RunAPIKeyRepositoryTests(t, openTestStore)
	storetest.RunAuditRepositoryTests(t, openTestStore)
	storetest.RunAutomationRepositoryTests(t, openTestStore)
	storetest.RunStorageRepositoryTests(t, openTestStore)
	storetest.RunConversationRepositoryTests(t, openTestStore)
}

func openTestStore(t *testing.T) repositorycontract.Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.DB().Close()
	})
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return st
}
