package settings

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

func TestCombinedPatchRollsBackAllConfigurationKeys(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "settings.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	first := settingscore.New(store, integerSettingsSpec("first", "settings.first", "first_value"))
	second := settingscore.New(store, integerSettingsSpec("second", "settings.second", "second_value"))
	combined, err := Combine("combined", "Combined", first, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		CREATE TRIGGER reject_second_config BEFORE INSERT ON config
		WHEN NEW.key='settings.second'
		BEGIN SELECT RAISE(ABORT, 'injected batch failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := combined.Patch(ctx, map[string]any{"first_value": 1, "second_value": 2}); err == nil {
		t.Fatal("combined patch unexpectedly succeeded")
	}
	if _, err := store.GetSystemConfig(ctx, "settings.first"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("first config was partially committed: %v", err)
	}
}

func integerSettingsSpec(domain, storageKey, fieldKey string) settingscore.Spec {
	minimum := float64(0)
	return settingscore.Spec{
		Domain: domain, Title: domain, StorageKey: storageKey,
		Defaults: map[string]any{fieldKey: 0},
		Fields:   []settingscore.Descriptor{{Key: fieldKey, Type: "integer", Editable: true, Minimum: &minimum, Default: 0}},
	}
}
