package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/repository/migrations"
	pgstore "github.com/zyf/chatapi/internal/repository/postgresql"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
)

func TestStorageMonitorSummaryIncludesSQLiteDatabaseInfo(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "chatapi.sqlite3")
	st, err := sqlitestore.Open(dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite: %v", err)
	}

	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.DatabaseDriver = "sqlite"
	cfg.DatabaseDSN = dsn

	monitor := NewStorageMonitorService(cfg, st)
	summary, err := monitor.Summary(context.Background())
	if err != nil {
		t.Fatalf("storage summary: %v", err)
	}
	if summary.Database.Driver != "sqlite" {
		t.Fatalf("unexpected database driver: %#v", summary.Database)
	}
	if summary.Database.SQLitePath != dsn {
		t.Fatalf("unexpected sqlite path: %#v", summary.Database)
	}
	if summary.Database.PostgresMaxConns != 0 || summary.Database.PostgresTotalConns != 0 {
		t.Fatalf("sqlite summary should not expose postgres pool stats: %#v", summary.Database)
	}
}

func TestStorageMonitorSummaryIncludesPostgreSQLPoolInfo(t *testing.T) {
	dsn := os.Getenv("CHATAPI_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("CHATAPI_PG_TEST_DSN is not set")
	}
	ctx := context.Background()
	st, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgresql: %v", err)
	}
	t.Cleanup(st.Close)
	if err := pgstore.Reset(ctx, st.Pool()); err != nil {
		t.Fatalf("reset postgresql: %v", err)
	}
	if err := pgstore.Bootstrap(ctx, st.Pool()); err != nil {
		t.Fatalf("bootstrap postgresql: %v", err)
	}

	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.DatabaseDriver = "postgresql"
	cfg.DatabaseDSN = dsn

	monitor := NewStorageMonitorService(cfg, st)
	summary, err := monitor.Summary(ctx)
	if err != nil {
		t.Fatalf("storage summary: %v", err)
	}
	if summary.Database.Driver != "postgresql" {
		t.Fatalf("unexpected postgres database driver: %#v", summary.Database)
	}
	if summary.Database.PostgresMaxConns <= 0 {
		t.Fatalf("expected postgres pool max conns: %#v", summary.Database)
	}
	if summary.Database.SQLitePath != "" || summary.Database.SQLiteWAL != "" {
		t.Fatalf("postgres summary should not expose sqlite paths: %#v", summary.Database)
	}
}

func TestStorageMonitorVacuumRejectsPostgreSQL(t *testing.T) {
	dsn := os.Getenv("CHATAPI_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("CHATAPI_PG_TEST_DSN is not set")
	}
	ctx := context.Background()
	st, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgresql: %v", err)
	}
	t.Cleanup(st.Close)
	if err := pgstore.Reset(ctx, st.Pool()); err != nil {
		t.Fatalf("reset postgresql: %v", err)
	}
	if err := pgstore.Bootstrap(ctx, st.Pool()); err != nil {
		t.Fatalf("bootstrap postgresql: %v", err)
	}

	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.DatabaseDriver = "postgresql"
	cfg.DatabaseDSN = dsn

	monitor := NewStorageMonitorService(cfg, st)
	preview, err := monitor.Vacuum(ctx, true)
	if err != nil {
		t.Fatalf("postgres vacuum dry-run: %v", err)
	}
	if preview.Before.Driver != "postgresql" || preview.After != nil {
		t.Fatalf("unexpected postgres vacuum dry-run response: %#v", preview)
	}

	_, err = monitor.Vacuum(ctx, false)
	if !errors.Is(err, ErrStorageVacuumUnsupported) {
		t.Fatalf("expected postgres vacuum unsupported error, got %v", err)
	}
}
