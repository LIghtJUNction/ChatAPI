package settings_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	"github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	chatsettings "github.com/zyf2007/ChatAPI/internal/service/chat/settings"
)

func TestOutputEventLimitRoundTrip(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(context.Background(), store.DB()); err != nil {
		t.Fatal(err)
	}
	service := chatsettings.New(store, config.Config{})
	if _, _, err := service.Patch(context.Background(), map[string]any{"max_output_events_per_message": 25}); err != nil {
		t.Fatal(err)
	}
	current, err := service.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.MaxOutputEventsPerMessage != 25 {
		t.Fatalf("unexpected output event limit: %#v", current)
	}
}
