package bootstrap

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/config"
)

func TestRunWaitsForWorkersBeforeClosingResourcesOnListenFailure(t *testing.T) {
	workerStarted := make(chan struct{})
	workerStopped := make(chan struct{})
	var mu sync.Mutex
	closed := make([]string, 0, 2)
	recordClose := func(resource string) {
		select {
		case <-workerStopped:
		default:
			t.Errorf("%s closed before worker stopped", resource)
		}
		mu.Lock()
		closed = append(closed, resource)
		mu.Unlock()
	}
	app := &App{
		Config:  config.Config{Host: "127.0.0.1", Port: -1},
		Handler: http.NotFoundHandler(),
		logger:  zap.NewNop(),
		workers: []func(context.Context){func(ctx context.Context) {
			close(workerStarted)
			<-ctx.Done()
			close(workerStopped)
		}},
		notifications: closeFunc(func() error { recordClose("notification"); return nil }),
		storeClose:    func() { recordClose("store") },
	}

	if err := app.Run(context.Background()); err == nil {
		t.Fatal("expected listen failure")
	}
	select {
	case <-workerStarted:
	default:
		t.Fatal("worker was not started")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(closed) != 2 || closed[0] != "notification" || closed[1] != "store" {
		t.Fatalf("unexpected close order: %v", closed)
	}
	if _, err := app.startWorkers(context.Background()); err == nil {
		t.Fatal("closed app unexpectedly restarted")
	}
}

type closeFunc func() error

func (f closeFunc) Close() error { return f() }

func TestNewBuildsCompleteLabApp(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHATAPI_DB_DRIVER", "sqlite")
	t.Setenv("CHATAPI_DB_DSN", filepath.Join(root, "chatapi.sqlite3"))
	t.Setenv("CHATAPI_DATA_DIR", root)
	t.Setenv("CHATAPI_MEDIA_DERIVED_DIR", filepath.Join(root, "media"))
	t.Setenv("CHATAPI_PORT", "0")
	t.Setenv("CHATAPI_SESSION_SECRET", "test-session-secret-at-least-32-characters")

	app, err := New(context.Background(), Options{BackendRoot: root, Mode: config.ModeLab})
	if err != nil {
		t.Fatalf("build lab app: %v", err)
	}
	t.Cleanup(app.Close)
	if app.Handler == nil || app.services.Turn == nil || app.services.ChatSettings == nil || app.services.Audit == nil {
		t.Fatalf("incomplete app graph: %#v", app.services)
	}
}

func TestDetectBackendRootFromFindsGoModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := DetectBackendRootFrom(nested); got != root {
		t.Fatalf("root = %q, want %q", got, root)
	}
}

func TestNextDailyRun(t *testing.T) {
	location := time.FixedZone("test", 8*60*60)
	cases := []struct {
		name   string
		now    time.Time
		hour   int
		minute int
		want   time.Time
	}{
		{
			name: "later today", now: time.Date(2026, time.July, 23, 2, 30, 0, 0, location),
			hour: 3, minute: 0, want: time.Date(2026, time.July, 23, 3, 0, 0, 0, location),
		},
		{
			name: "next day after scheduled time", now: time.Date(2026, time.July, 23, 3, 0, 0, 0, location),
			hour: 3, minute: 0, want: time.Date(2026, time.July, 24, 3, 0, 0, 0, location),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextDailyRun(tc.now, tc.hour, tc.minute); !got.Equal(tc.want) {
				t.Fatalf("next daily run = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestStorageVacuumEnabledOnlyForSQLite(t *testing.T) {
	if !StorageVacuumEnabled(config.Config{DatabaseDriver: "sqlite", StorageCleanupEnabled: true, StorageVacuumEnabled: true}) {
		t.Fatal("expected SQLite vacuum to be enabled")
	}
	if StorageVacuumEnabled(config.Config{DatabaseDriver: "postgresql", StorageCleanupEnabled: true, StorageVacuumEnabled: true}) {
		t.Fatal("PostgreSQL must not use the SQLite vacuum scheduler")
	}
}

func TestDetectBackendRootFromSupportsStandaloneBinary(t *testing.T) {
	start := t.TempDir()
	if got := DetectBackendRootFrom(start); got != start {
		t.Fatalf("root = %q, want standalone cwd %q", got, start)
	}
}

func TestModeFromArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want config.Mode
	}{
		{want: config.ModeServe},
		{args: []string{"serve"}, want: config.ModeServe},
		{args: []string{"lab"}, want: config.ModeLab},
	} {
		got, err := ModeFromArgs(tc.args)
		if err != nil || got != tc.want {
			t.Fatalf("ModeFromArgs(%v) = %q, %v; want %q", tc.args, got, err, tc.want)
		}
	}
	if _, err := ModeFromArgs([]string{"unknown"}); err == nil {
		t.Fatal("expected unknown mode error")
	}
}
