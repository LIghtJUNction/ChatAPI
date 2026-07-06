package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/store"
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

func TestEnsureSessionSecretKeepsEnvValue(t *testing.T) {
	ctx := context.Background()
	st := openAppTestStore(t)
	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.SessionSecret = "env-session-secret"

	if err := ensureSessionSecret(ctx, &cfg, st, nil); err != nil {
		t.Fatalf("ensure session secret: %v", err)
	}
	if cfg.SessionSecret != "env-session-secret" {
		t.Fatalf("unexpected session secret: %q", cfg.SessionSecret)
	}
	if _, err := st.GetSystemConfig(ctx, sessionSecretConfigKey); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("env session secret should not be persisted by ensure, got %v", err)
	}
}

func TestEnsureSessionSecretLoadsPersistedValue(t *testing.T) {
	ctx := context.Background()
	st := openAppTestStore(t)
	if _, err := st.SetSystemConfig(ctx, store.SetSystemConfigInput{
		Key: sessionSecretConfigKey,
		Value: map[string]any{
			"secret": "persisted-session-secret",
		},
	}); err != nil {
		t.Fatalf("seed session secret: %v", err)
	}
	cfg := config.Default(config.ModeServe, t.TempDir())

	if err := ensureSessionSecret(ctx, &cfg, st, nil); err != nil {
		t.Fatalf("ensure session secret: %v", err)
	}
	if cfg.SessionSecret != "persisted-session-secret" {
		t.Fatalf("unexpected session secret: %q", cfg.SessionSecret)
	}
}

func TestEnsureSessionSecretGeneratesAndPersistsServeSecret(t *testing.T) {
	ctx := context.Background()
	st := openAppTestStore(t)
	cfg := config.Default(config.ModeServe, t.TempDir())

	if err := ensureSessionSecret(ctx, &cfg, st, nil); err != nil {
		t.Fatalf("ensure session secret: %v", err)
	}
	if len(cfg.SessionSecret) < 32 {
		t.Fatalf("expected generated session secret, got %q", cfg.SessionSecret)
	}
	item, err := st.GetSystemConfig(ctx, sessionSecretConfigKey)
	if err != nil {
		t.Fatalf("load persisted session secret: %v", err)
	}
	if item.Value["secret"] != cfg.SessionSecret || item.Value["generated_at"] == "" {
		t.Fatalf("unexpected persisted session secret: %#v", item)
	}
}

func TestEnsureSessionSecretUsesLabDefaultWithoutPersistence(t *testing.T) {
	ctx := context.Background()
	st := openAppTestStore(t)
	cfg := config.Default(config.ModeLab, t.TempDir())

	if err := ensureSessionSecret(ctx, &cfg, st, nil); err != nil {
		t.Fatalf("ensure lab session secret: %v", err)
	}
	if cfg.SessionSecret != "chatapi-lab-insecure-session-secret" {
		t.Fatalf("unexpected lab session secret: %q", cfg.SessionSecret)
	}
	if _, err := st.GetSystemConfig(ctx, sessionSecretConfigKey); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("lab session secret should not be persisted, got %v", err)
	}
}

func TestParseSMTPTestOptionsRejectsDryRunWithConnectOnly(t *testing.T) {
	_, err := parseSMTPTestOptions([]string{"--dry-run", "--connect-only"})
	if err == nil {
		t.Fatal("expected incompatible smtp options error")
	}
}

func TestOIDCTestCommandDiscoverySuccess(t *testing.T) {
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			t.Fatalf("unexpected discovery path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer": "` + issuer + `",
			"authorization_endpoint": "` + issuer + `/authorize",
			"token_endpoint": "` + issuer + `/token",
			"jwks_uri": "` + issuer + `/jwks",
			"userinfo_endpoint": "` + issuer + `/userinfo"
		}`))
	}))
	defer server.Close()
	issuer = server.URL

	report, err := oidcTestCommand(context.Background(), config.Config{
		OIDCIssuerURL:    issuer,
		OIDCClientID:     "chatapi",
		OIDCClientSecret: "secret",
		OIDCRedirectURL:  "http://localhost/callback",
		OIDCScopes:       []string{"openid", "email"},
	}, server.Client())
	if err != nil {
		t.Fatalf("oidc test: %v report=%#v", err, report)
	}
	if !report.OK || report.ProviderIssuer != issuer || report.AuthorizationEndpoint == "" || report.TokenEndpoint == "" || report.JWKSURI == "" {
		t.Fatalf("unexpected oidc report: %#v", report)
	}
	if report.ClientSecretConfigured != true {
		t.Fatalf("client secret should only be reported as configured: %#v", report)
	}
}

func TestOIDCTestCommandDetectsIssuerMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer": "https://idp.example.com",
			"authorization_endpoint": "https://idp.example.com/authorize",
			"token_endpoint": "https://idp.example.com/token",
			"jwks_uri": "https://idp.example.com/jwks"
		}`))
	}))
	defer server.Close()

	report, err := oidcTestCommand(context.Background(), config.Config{
		OIDCIssuerURL:    server.URL,
		OIDCClientID:     "chatapi",
		OIDCClientSecret: "secret",
		OIDCRedirectURL:  "http://localhost/callback",
	}, server.Client())
	if err == nil {
		t.Fatal("expected issuer mismatch")
	}
	if report.OK || !strings.Contains(strings.Join(report.Errors, "; "), "issuer") {
		t.Fatalf("unexpected mismatch report: %#v", report)
	}
}

func TestOIDCTestCommandRequiresPrivateRPConfig(t *testing.T) {
	report, err := oidcTestCommand(context.Background(), config.Config{}, http.DefaultClient)
	if err == nil {
		t.Fatal("expected missing oidc config error")
	}
	if report.OK || !strings.Contains(strings.Join(report.Errors, "; "), "CHATAPI_OIDC_ISSUER_URL") {
		t.Fatalf("unexpected missing config report: %#v", report)
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
	if !strings.Contains(report.EnvTemplate, "CHATAPI_MASTER_KEY=") || !strings.Contains(report.EnvTemplate, "CHATAPI_SESSION_SECRET=") || !strings.Contains(report.EnvTemplate, "CHATAPI_ADMIN_PASSWORD=") {
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
	if !strings.Contains(content, "CHATAPI_MASTER_KEY=") || !strings.Contains(content, "CHATAPI_SESSION_SECRET=") || !strings.Contains(content, "CHATAPI_ADMIN_PASSWORD=") {
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

func TestDBCheckCommandReportsSQLiteFiles(t *testing.T) {
	backendRoot := t.TempDir()
	dbPath := filepath.Join(backendRoot, "data", "chatapi.sqlite3")
	t.Setenv("CHATAPI_DB_DRIVER", "sqlite")
	t.Setenv("CHATAPI_DB_DSN", dbPath)
	t.Setenv("CHATAPI_DATA_DIR", filepath.Join(backendRoot, "data"))

	report, err := dbCheckCommand(backendRoot)
	if err != nil {
		t.Fatalf("db check: %v report=%#v", err, report)
	}
	if !report.OK || report.Status.SchemaVersion != migrations.BootstrapVersion {
		t.Fatalf("unexpected db check report: %#v", report)
	}
	if report.SQLite.Database.Path != dbPath || !report.SQLite.Database.Exists || report.SQLite.Database.Bytes <= 0 {
		t.Fatalf("unexpected sqlite database info: %#v", report.SQLite)
	}
	if report.SQLite.WAL.Path != dbPath+"-wal" || report.SQLite.SHM.Path != dbPath+"-shm" {
		t.Fatalf("unexpected sqlite sidecar paths: %#v", report.SQLite)
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

func openAppTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.DB().Close()
	})
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite store: %v", err)
	}
	return st
}
