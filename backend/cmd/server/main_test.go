package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/config"
)

func TestDetectBackendRootFromFindsGoModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := detectBackendRootFrom(nested); got != root {
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
			name: "later today",
			now:  time.Date(2026, time.July, 23, 2, 30, 0, 0, location),
			hour: 3, minute: 0,
			want: time.Date(2026, time.July, 23, 3, 0, 0, 0, location),
		},
		{
			name: "next day after scheduled time",
			now:  time.Date(2026, time.July, 23, 3, 0, 0, 0, location),
			hour: 3, minute: 0,
			want: time.Date(2026, time.July, 24, 3, 0, 0, 0, location),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextDailyRun(tc.now, tc.hour, tc.minute); !got.Equal(tc.want) {
				t.Fatalf("next daily run = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestStorageVacuumEnabledOnlyForSQLite(t *testing.T) {
	if !storageVacuumEnabled(config.Config{DatabaseDriver: "sqlite", StorageCleanupEnabled: true, StorageVacuumEnabled: true}) {
		t.Fatal("expected SQLite vacuum to be enabled")
	}
	if storageVacuumEnabled(config.Config{DatabaseDriver: "postgresql", StorageCleanupEnabled: true, StorageVacuumEnabled: true}) {
		t.Fatal("PostgreSQL must not use the SQLite vacuum scheduler")
	}
}

func TestDetectBackendRootFromSupportsStandaloneBinary(t *testing.T) {
	start := t.TempDir()
	if got := detectBackendRootFrom(start); got != start {
		t.Fatalf("root = %q, want standalone cwd %q", got, start)
	}
}
