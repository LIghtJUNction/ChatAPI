package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zyf/chatapi/internal/repository/migrations"
	"github.com/zyf/chatapi/internal/repository/storetest"
	"github.com/zyf/chatapi/internal/store"
)

func TestSQLiteRepositoryContracts(t *testing.T) {
	storetest.RunUserRepositoryTests(t, openTestStore)
	storetest.RunConfigRepositoryTests(t, openTestStore)
}

func openTestStore(t *testing.T) store.Store {
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
