package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/repository/migrations"
	pgstore "github.com/zyf/chatapi/internal/repository/postgresql"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/testutil/pgtest"
)

func TestRuntimeMonitorSummaryIncludesSQLiteDatabaseInfo(t *testing.T) {
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

	monitor := NewRuntimeMonitorService(cfg, st, NewRealtimeHub(st), NewPendingRegistry())
	summary := monitor.Summary()
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

func TestMetricsServiceIncludesSQLiteDatabaseMetrics(t *testing.T) {
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

	monitor := NewRuntimeMonitorService(cfg, st, NewRealtimeHub(st), NewPendingRegistry())
	observer := NewAutomationObserver()
	observer.RecordNoRules()
	observer.RecordNoMatch()
	monitor.SetAutomationObserver(observer)
	metrics := NewMetricsService(monitor, NewHTTPMetricsRegistry()).PrometheusText()
	for _, expected := range []string{
		"chatapi_sqlite_database_bytes",
		"chatapi_sqlite_wal_bytes",
		"chatapi_automation_failures_total",
		"chatapi_automation_no_rules_total",
		"chatapi_automation_no_match_total",
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("expected sqlite metrics %q in body:\n%s", expected, metrics)
		}
	}
}

func TestRuntimeMonitorSummaryIncludesPostgreSQLPoolInfo(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
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

	monitor := NewRuntimeMonitorService(cfg, st, NewRealtimeHub(st), NewPendingRegistry())
	summary := monitor.Summary()
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

func TestMetricsServiceIncludesPostgreSQLPoolMetrics(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
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

	monitor := NewRuntimeMonitorService(cfg, st, NewRealtimeHub(st), NewPendingRegistry())
	observer := NewAutomationObserver()
	observer.RecordNoRules()
	monitor.SetAutomationObserver(observer)
	metrics := NewMetricsService(monitor, NewHTTPMetricsRegistry()).PrometheusText()
	for _, expected := range []string{
		"chatapi_postgres_pool_max_conns",
		"chatapi_postgres_pool_total_conns",
		"chatapi_postgres_pool_acquired_conns",
		"chatapi_postgres_pool_idle_conns",
		"chatapi_automation_no_rules_total",
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("expected postgres metric %q in body:\n%s", expected, metrics)
		}
	}
}
