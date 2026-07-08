package access_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
)

func TestAccessSettingsRoundTrip(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}

	service := authaccess.NewSettingsService(st, authaccess.Settings{})
	item, err := service.Set(context.Background(), map[string]any{
		"user_rate_limit_requests":      12,
		"user_rate_limit_window":        "1m",
		"session_rate_limit_requests":   24,
		"session_rate_limit_window":     "2m",
		"app_key_rate_limit_requests":   36,
		"app_key_rate_limit_window":     "3m",
		"model_key_rate_limit_requests": 48,
		"model_key_rate_limit_window":   "4m",
	})
	if err != nil {
		t.Fatalf("set settings: %v", err)
	}
	if item["user_rate_limit_requests"].(int) != 12 {
		t.Fatalf("unexpected settings map: %#v", item)
	}

	value, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if value.UserRateLimitRequests != 12 || value.UserRateLimitWindow != time.Minute {
		t.Fatalf("unexpected user limits: %#v", value)
	}
	if value.ModelKeyRateLimitRequests != 48 || value.ModelKeyRateLimitWindow != 4*time.Minute {
		t.Fatalf("unexpected model limits: %#v", value)
	}
}
