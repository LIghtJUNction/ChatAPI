package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestDurationUntilDailyRun(t *testing.T) {
	now := time.Date(2026, 7, 6, 2, 30, 0, 0, time.UTC)
	duration, err := durationUntilDailyRun(now, "03:00")
	if err != nil {
		t.Fatalf("duration until daily run: %v", err)
	}
	if duration != 30*time.Minute {
		t.Fatalf("unexpected duration: %s", duration)
	}

	duration, err = durationUntilDailyRun(now, "02:00")
	if err != nil {
		t.Fatalf("duration until next day run: %v", err)
	}
	if duration != 23*time.Hour+30*time.Minute {
		t.Fatalf("unexpected next day duration: %s", duration)
	}

	if _, err := durationUntilDailyRun(now, "25:00"); err == nil {
		t.Fatalf("expected invalid daily time error")
	}
}

func TestParseSMTPTestOptionsRejectsDryRunWithConnectOnly(t *testing.T) {
	_, err := parseSMTPTestOptions([]string{"--dry-run", "--connect-only"})
	if err == nil {
		t.Fatal("expected incompatible smtp options error")
	}
}

func TestSetupCommandDryRunDoesNotWriteEnv(t *testing.T) {
	backendRoot := t.TempDir()
	report, err := setupCommand(backendRoot, setupOptions{})
	if err != nil {
		t.Fatalf("setup dry-run: %v", err)
	}
	if !report.OK || report.Written || report.EnvTemplate == "" {
		t.Fatalf("unexpected setup dry-run report: %#v", report)
	}
	if !strings.Contains(report.EnvTemplate, "CHATAPI_MASTER_KEY=") || !strings.Contains(report.EnvTemplate, "CHATAPI_ADMIN_PASSWORD=") {
		t.Fatalf("setup template missing required secrets: %q", report.EnvTemplate)
	}
	if _, err := os.Stat(filepath.Join(backendRoot, ".env")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not write .env, err=%v", err)
	}
}

func TestSetupCommandWriteEnv(t *testing.T) {
	backendRoot := t.TempDir()
	report, err := setupCommand(backendRoot, setupOptions{writeEnv: true})
	if err != nil {
		t.Fatalf("setup write-env: %v", err)
	}
	if !report.OK || !report.Written || report.EnvTemplate != "" {
		t.Fatalf("unexpected setup write report: %#v", report)
	}
	data, err := os.ReadFile(filepath.Join(backendRoot, ".env"))
	if err != nil {
		t.Fatalf("read written .env: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "CHATAPI_MASTER_KEY=") || !strings.Contains(content, "CHATAPI_ADMIN_PASSWORD=") {
		t.Fatalf("written .env missing required values: %q", content)
	}
}

func TestSetupCommandRejectsExistingEnvWithoutForce(t *testing.T) {
	backendRoot := t.TempDir()
	envPath := filepath.Join(backendRoot, ".env")
	if err := os.WriteFile(envPath, []byte("CHATAPI_MASTER_KEY=keep\n"), 0o600); err != nil {
		t.Fatalf("seed .env: %v", err)
	}
	report, err := setupCommand(backendRoot, setupOptions{writeEnv: true})
	if err == nil {
		t.Fatal("expected existing .env rejection")
	}
	if report.OK || !strings.Contains(report.Error, "already exists") {
		t.Fatalf("unexpected existing .env setup report: %#v", report)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read existing .env: %v", err)
	}
	if string(data) != "CHATAPI_MASTER_KEY=keep\n" {
		t.Fatalf("setup should not overwrite existing .env: %q", string(data))
	}
}

func TestMigrateCommandUpBootstrapsSQLite(t *testing.T) {
	backendRoot := t.TempDir()
	t.Setenv("CHATAPI_DB_DRIVER", "sqlite")
	t.Setenv("CHATAPI_DB_DSN", filepath.Join(backendRoot, "data", "chatapi.sqlite3"))
	t.Setenv("CHATAPI_DATA_DIR", filepath.Join(backendRoot, "data"))

	report, err := migrateCommand(context.Background(), migrateOptions{command: "up"}, backendRoot)
	if err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !report.OK || report.Command != "up" || report.Driver != "sqlite" {
		t.Fatalf("unexpected migrate report: %#v", report)
	}
	if report.Status.SchemaVersion != migrations.BootstrapVersion || report.Status.MigrationDirty {
		t.Fatalf("unexpected migration status: %#v", report.Status)
	}

	statusReport, err := migrateCommand(context.Background(), migrateOptions{command: "status"}, backendRoot)
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

	report, err := migrateCommand(context.Background(), migrateOptions{command: "status"}, backendRoot)
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

	report, err := migrateCommand(context.Background(), migrateOptions{command: "status"}, backendRoot)
	if err == nil {
		t.Fatal("expected unsupported driver error")
	}
	if report.OK || !strings.Contains(report.Error, "only sqlite migration is implemented") {
		t.Fatalf("unexpected unsupported driver report: %#v", report)
	}
}

func TestParseMigrateOptionsRequiresForceForDown(t *testing.T) {
	_, err := parseMigrateOptions([]string{"down"})
	if err == nil {
		t.Fatal("expected migrate down without force to fail")
	}
	options, err := parseMigrateOptions([]string{"down", "--force"})
	if err != nil {
		t.Fatalf("parse migrate down force: %v", err)
	}
	if options.command != "down" || !options.force {
		t.Fatalf("unexpected migrate options: %#v", options)
	}
}

func TestMigrateCommandDownResetsSQLite(t *testing.T) {
	backendRoot := t.TempDir()
	t.Setenv("CHATAPI_DB_DRIVER", "sqlite")
	t.Setenv("CHATAPI_DB_DSN", filepath.Join(backendRoot, "data", "chatapi.sqlite3"))
	t.Setenv("CHATAPI_DATA_DIR", filepath.Join(backendRoot, "data"))

	if _, err := migrateCommand(context.Background(), migrateOptions{command: "up"}, backendRoot); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	report, err := migrateCommand(context.Background(), migrateOptions{command: "down", force: true}, backendRoot)
	if err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if !report.OK || report.Command != "down" || !report.Forced || report.Status.SchemaVersion != migrations.BootstrapVersion {
		t.Fatalf("unexpected migrate down report: %#v", report)
	}
	statusReport, err := migrateCommand(context.Background(), migrateOptions{command: "status"}, backendRoot)
	if err == nil {
		t.Fatal("expected status after migrate down to fail")
	}
	if statusReport.OK || !strings.Contains(statusReport.Error, "read db_meta") {
		t.Fatalf("unexpected status after down: %#v", statusReport)
	}
}
