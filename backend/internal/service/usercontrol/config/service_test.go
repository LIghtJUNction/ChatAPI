package config_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	"github.com/zyf2007/ChatAPI/internal/repository/repositorycontract"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	userconfig "github.com/zyf2007/ChatAPI/internal/service/usercontrol/config"
)

func TestConfigServiceGetAndUpdate(t *testing.T) {
	st := openConfigStore(t)
	ctx := context.Background()
	svc := userconfig.New(userconfig.Deps{Configs: st, Chat: st})
	original := map[string]any{"theme": "dark", "nested": map[string]any{"x": 1}}

	item, err := svc.UpdateUserConfig(ctx, " user_a ", original)
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	if item.UserID != "user_a" || item.Key != "settings" {
		t.Fatalf("unexpected config item: %#v", item)
	}
	item.Value["theme"] = "light"
	if original["theme"] != "dark" {
		t.Fatalf("input map should not be mutated: %#v", original)
	}

	got, err := svc.GetUserConfig(ctx, "user_a")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if got.Value["theme"] != "dark" {
		t.Fatalf("unexpected config value: %#v", got.Value)
	}

}

func openConfigStore(t *testing.T) repositorycontract.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}
	return st
}
