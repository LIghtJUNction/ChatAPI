package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyf/chatapi/internal/repository/migrations"
)

func TestParseSMTPTestOptionsConnectOnly(t *testing.T) {
	options, err := parseSMTPTestOptions([]string{"--connect-only", "--to", "user@example.com", "--subject", "hello"})
	if err != nil {
		t.Fatalf("parse smtp options: %v", err)
	}
	if !options.connectOnly || options.dryRun {
		t.Fatalf("unexpected mode flags: %#v", options)
	}
	if options.to != "user@example.com" || options.subject != "hello" {
		t.Fatalf("unexpected smtp options: %#v", options)
	}
}

func TestParseSMTPTestOptionsRejectsDryRunWithConnectOnly(t *testing.T) {
	_, err := parseSMTPTestOptions([]string{"--dry-run", "--connect-only"})
	if err == nil {
		t.Fatal("expected incompatible smtp options error")
	}
}

func TestMigrateCommandUpBootstrapsSQLite(t *testing.T) {
	backendRoot := t.TempDir()
	t.Setenv("CHATAPI_DB_DRIVER", "sqlite")
	t.Setenv("CHATAPI_DB_DSN", filepath.Join(backendRoot, "data", "chatapi.sqlite3"))
	t.Setenv("CHATAPI_DATA_DIR", filepath.Join(backendRoot, "data"))

	report, err := migrateCommand(context.Background(), "up", backendRoot)
	if err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !report.OK || report.Command != "up" || report.Driver != "sqlite" {
		t.Fatalf("unexpected migrate report: %#v", report)
	}
	if report.Status.SchemaVersion != migrations.BootstrapVersion || report.Status.MigrationDirty {
		t.Fatalf("unexpected migration status: %#v", report.Status)
	}

	statusReport, err := migrateCommand(context.Background(), "status", backendRoot)
	if err != nil {
		t.Fatalf("migrate status: %v", err)
	}
	if statusReport.Status.SchemaVersion != migrations.BootstrapVersion {
		t.Fatalf("status should see bootstrapped schema: %#v", statusReport)
	}
}

func TestMigrateCommandStatusDoesNotBootstrapSQLite(t *testing.T) {
	backendRoot := t.TempDir()
	t.Setenv("CHATAPI_DB_DRIVER", "sqlite")
	t.Setenv("CHATAPI_DB_DSN", filepath.Join(backendRoot, "data", "chatapi.sqlite3"))
	t.Setenv("CHATAPI_DATA_DIR", filepath.Join(backendRoot, "data"))

	report, err := migrateCommand(context.Background(), "status", backendRoot)
	if err == nil {
		t.Fatal("expected status before migrate up to fail")
	}
	if report.OK || !strings.Contains(report.Error, "read db_meta") {
		t.Fatalf("unexpected status report before bootstrap: %#v", report)
	}
}

func TestMigrateCommandRejectsUnsupportedDriver(t *testing.T) {
	backendRoot := t.TempDir()
	t.Setenv("CHATAPI_DB_DRIVER", "postgresql")

	report, err := migrateCommand(context.Background(), "status", backendRoot)
	if err == nil {
		t.Fatal("expected unsupported driver error")
	}
	if report.OK || !strings.Contains(report.Error, "only sqlite migration is implemented") {
		t.Fatalf("unexpected unsupported driver report: %#v", report)
	}
}
