package app

import (
	"context"
	"encoding/json"
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
	pgstore "github.com/zyf/chatapi/internal/repository/postgresql"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
	"github.com/zyf/chatapi/internal/testutil/pgtest"
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

func TestApplyLabOptionsSupportsPortZeroAndPassword(t *testing.T) {
	cfg := config.Default(config.ModeLab, t.TempDir())
	if err := applyRuntimeOptions(&cfg, []string{"--host", "0.0.0.0", "--port", "0", "--allow-remote-lab", "--password", "dev-password", "--no-open-browser"}); err != nil {
		t.Fatalf("apply lab options: %v", err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != 0 || !cfg.AllowRemoteLab {
		t.Fatalf("unexpected lab option values: %#v", cfg)
	}
	if cfg.LabPassword != "dev-password" || cfg.LabToken != "" || cfg.OpenBrowser {
		t.Fatalf("unexpected lab auth/browser options: %#v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("lab config should validate with port zero: %v", err)
	}
}

func TestApplyLabOptionsPasswordClearsToken(t *testing.T) {
	cfg := config.Default(config.ModeLab, t.TempDir())
	cfg.LabToken = "seed-token"
	if err := applyRuntimeOptions(&cfg, []string{"--password", "dev-password"}); err != nil {
		t.Fatalf("apply lab password option: %v", err)
	}
	if cfg.LabPassword != "dev-password" || cfg.LabToken != "" {
		t.Fatalf("password mode should clear token: %#v", cfg)
	}
}

func TestApplyServeOptionsOverridesListenAddr(t *testing.T) {
	cfg := config.Default(config.ModeServe, t.TempDir())
	if err := applyRuntimeOptions(&cfg, []string{"--host", "127.0.0.1", "--port", "8080", "--no-open-browser"}); err != nil {
		t.Fatalf("apply serve options: %v", err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 8080 || cfg.OpenBrowser {
		t.Fatalf("unexpected serve options: %#v", cfg)
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

func TestShouldRunStorageDBMaintenance(t *testing.T) {
	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.DatabaseDriver = "sqlite"
	if !shouldRunStorageDBMaintenance(cfg) {
		t.Fatal("sqlite should enable scheduled db maintenance")
	}

	cfg.DatabaseDriver = "postgresql"
	if shouldRunStorageDBMaintenance(cfg) {
		t.Fatal("postgresql should skip sqlite-specific scheduled db maintenance")
	}

	cfg.DatabaseDriver = "PoStGrEs"
	if shouldRunStorageDBMaintenance(cfg) {
		t.Fatal("postgres alias should skip sqlite-specific scheduled db maintenance")
	}
}

func TestRuntimeMonitorSQLiteDatabaseInfo(t *testing.T) {
	tempDir := t.TempDir()
	dsn := filepath.Join(tempDir, "chatapi.sqlite3")
	st, err := sqlitestore.Open(dsn)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite store: %v", err)
	}
	cfg := config.Default(config.ModeServe, tempDir)
	cfg.DatabaseDriver = "sqlite"
	cfg.DatabaseDSN = dsn
	monitor := service.NewRuntimeMonitorService(cfg, st, service.NewRealtimeHub(st), service.NewPendingRegistry())
	info := monitor.Summary().Database
	if info.Driver != "sqlite" {
		t.Fatalf("unexpected driver: %#v", info)
	}
	if info.SQLitePath == "" {
		t.Fatalf("expected sqlite path in database info: %#v", info)
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
	if report.Status.SchemaVersion != migrations.LatestVersion || report.Status.MigrationDirty {
		t.Fatalf("unexpected migration status: %#v", report.Status)
	}

	statusReport, err := migrateCommand(context.Background(), migrateOptions{command: "status"}, backendRoot)
	if err != nil {
		t.Fatalf("migrate status: %v", err)
	}
	if statusReport.Status.SchemaVersion != migrations.LatestVersion {
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
	if !report.OK || report.Status.SchemaVersion != migrations.LatestVersion {
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

func TestMigrateCommandRejectsPostgreSQLWithoutDSN(t *testing.T) {
	backendRoot := t.TempDir()
	t.Setenv("CHATAPI_DB_DRIVER", "postgresql")

	report, err := migrateCommand(context.Background(), migrateOptions{command: "status"}, backendRoot)
	if err == nil {
		t.Fatal("expected postgresql dsn error")
	}
	if report.OK || !strings.Contains(report.Error, "postgresql database dsn is required") {
		t.Fatalf("unexpected postgresql report: %#v", report)
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
	if !report.OK || report.Command != "down" || !report.Forced || report.Status.SchemaVersion != migrations.LatestVersion {
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

func TestRuntimeMigrationStatusBootstrapsPostgreSQL(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
	backendRoot := t.TempDir()
	cfg := config.Default(config.ModeServe, backendRoot)
	cfg.DatabaseDriver = "postgresql"
	cfg.DatabaseDSN = dsn

	ctx := context.Background()
	pgStore, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgres store: %v", err)
	}
	t.Cleanup(pgStore.Close)
	if err := pgstore.Reset(ctx, pgStore.Pool()); err != nil {
		t.Fatalf("reset postgres schema: %v", err)
	}

	statusStore, closeStatusStore, err := openRuntimeStore(ctx, cfg, true)
	if err != nil {
		t.Fatalf("open runtime store with bootstrap: %v", err)
	}
	defer closeStatusStore()
	status, err := statusStore.MigrationStatus(ctx)
	if err != nil {
		t.Fatalf("postgres migration status: %v", err)
	}
	if status.SchemaVersion != pgstore.LatestVersion || status.MigrationDirty {
		t.Fatalf("unexpected postgres migration status: %#v", status)
	}
}

func TestDBCheckCommandReportsPostgreSQLStatus(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
	backendRoot := t.TempDir()
	t.Setenv("CHATAPI_DB_DRIVER", "postgresql")
	t.Setenv("CHATAPI_DB_DSN", dsn)
	t.Setenv("CHATAPI_DATA_DIR", filepath.Join(backendRoot, "data"))

	report, err := dbCheckCommand(backendRoot)
	if err != nil {
		t.Fatalf("postgres db check: %v report=%#v", err, report)
	}
	if !report.OK || report.Driver != "postgresql" || report.Status.SchemaVersion != pgstore.LatestVersion {
		t.Fatalf("unexpected postgres db check report: %#v", report)
	}
	if report.SQLite.Database.Path != "" || report.SQLite.WAL.Path != "" || report.SQLite.SHM.Path != "" {
		t.Fatalf("postgres db check should not include sqlite file info: %#v", report.SQLite)
	}
}

func TestMigrateCommandDownResetsPostgreSQL(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
	backendRoot := t.TempDir()
	t.Setenv("CHATAPI_DB_DRIVER", "postgresql")
	t.Setenv("CHATAPI_DB_DSN", dsn)

	if _, err := migrateCommand(context.Background(), migrateOptions{command: "up"}, backendRoot); err != nil {
		t.Fatalf("postgres migrate up: %v", err)
	}
	report, err := migrateCommand(context.Background(), migrateOptions{command: "down", force: true}, backendRoot)
	if err != nil {
		t.Fatalf("postgres migrate down: %v report=%#v", err, report)
	}
	if !report.OK || report.Command != "down" || !report.Forced || report.Status.SchemaVersion != pgstore.LatestVersion {
		t.Fatalf("unexpected postgres migrate down report: %#v", report)
	}
	statusReport, err := migrateCommand(context.Background(), migrateOptions{command: "status"}, backendRoot)
	if err == nil {
		t.Fatal("expected postgres status after migrate down to fail")
	}
	if statusReport.OK || !strings.Contains(statusReport.Error, "db_meta") {
		t.Fatalf("unexpected postgres status after down: %#v", statusReport)
	}
}

func TestParseMigrateDBOptions(t *testing.T) {
	options, err := parseMigrateDBOptions([]string{"sqlite-to-postgres", "--sqlite", "/tmp/chatapi.sqlite3", "--postgres", "postgres://example"})
	if err != nil {
		t.Fatalf("parse migrate-db options: %v", err)
	}
	if options.command != "sqlite-to-postgres" || options.sqlite != "/tmp/chatapi.sqlite3" || options.postgres != "postgres://example" {
		t.Fatalf("unexpected migrate-db options: %#v", options)
	}
	if _, err := parseMigrateDBOptions([]string{"sqlite-to-postgres", "--sqlite", "/tmp/chatapi.sqlite3"}); err == nil {
		t.Fatal("expected missing --postgres error")
	}
}

func TestMigrateDBCommandSQLiteToPostgreSQL(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
	sqlitePath := filepath.Join(t.TempDir(), "source.sqlite3")
	src, err := sqlitestore.Open(sqlitePath)
	if err != nil {
		t.Fatalf("open sqlite source store: %v", err)
	}
	t.Cleanup(func() { _ = src.Close() })
	if err := migrations.Bootstrap(context.Background(), src.DB()); err != nil {
		t.Fatalf("bootstrap sqlite source store: %v", err)
	}

	ctx := context.Background()
	now := time.Now().UTC()
	user, err := src.CreateUser(ctx, store.CreateUserInput{
		ID:           "user_migrate",
		Username:     "migrate-user",
		Email:        "migrate@example.com",
		PasswordHash: "argon2id$example",
		Role:         "admin",
		IsActive:     true,
		LocalAdmin:   true,
	})
	if err != nil {
		t.Fatalf("create sqlite user: %v", err)
	}
	if _, err := src.UpsertUserIdentity(ctx, store.UpsertUserIdentityInput{
		ID:            "ident_migrate",
		UserID:        user.ID,
		Provider:      "oidc",
		Subject:       "subject-1",
		Email:         user.Email,
		EmailVerified: true,
		Profile:       map[string]any{"name": "Migrate User"},
		LastLoginAt:   &now,
	}); err != nil {
		t.Fatalf("create sqlite identity: %v", err)
	}
	if _, err := src.SetSystemConfig(ctx, store.SetSystemConfigInput{
		Key:   "feature.flags",
		Value: map[string]any{"lab": true},
	}); err != nil {
		t.Fatalf("set sqlite system config: %v", err)
	}
	if _, err := src.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: user.ID,
		Key:    "ui.preferences",
		Value:  map[string]any{"theme": "dark"},
	}); err != nil {
		t.Fatalf("set sqlite user config: %v", err)
	}
	if _, err := src.CreateModelAPIKey(ctx, store.CreateModelAPIKeyInput{
		ID:            "mk_1",
		UserID:        user.ID,
		Name:          "virtual key",
		KeyCiphertext: "ciphertext",
		KeyPrefix:     "sk-test",
		Model:         "chatapi-lab",
	}); err != nil {
		t.Fatalf("create sqlite model key: %v", err)
	}
	if _, err := src.CreateAppAPIKey(ctx, store.CreateAppAPIKeyInput{
		ID:             "ak_1",
		UserID:         user.ID,
		Name:           "app key",
		KeyHash:        "hash",
		KeyPrefix:      "ak-test",
		Scopes:         []string{"requests:read"},
		ResourceLimits: map[string]any{"max_requests_per_minute": 12},
	}); err != nil {
		t.Fatalf("create sqlite app key: %v", err)
	}
	if err := src.CreateAppAPIKeyAuditLog(ctx, store.AppAPIKeyAuditLog{
		ID:          "akal_1",
		AppAPIKeyID: "ak_1",
		UserID:      user.ID,
		Route:       "/api/app/requests",
		StatusCode:  200,
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("create sqlite app api audit log: %v", err)
	}
	if _, err := src.CreateAuditLog(ctx, store.CreateAuditLogInput{
		ID:          "audit_1",
		ActorUserID: user.ID,
		ActorRole:   "admin",
		ActorSource: "session",
		EventType:   "user.config",
		Action:      "update",
		Outcome:     "success",
		Metadata:    map[string]any{"key": "ui.preferences"},
	}); err != nil {
		t.Fatalf("create sqlite audit log: %v", err)
	}
	if _, err := src.ReplaceAutomationRulesForUser(ctx, user.ID, map[string]struct{}{"rule_1": {}}, []store.UpsertAutomationRuleInput{{
		ID:      "rule_1",
		UserID:  user.ID,
		Enabled: true,
		Payload: map[string]any{"type": "echo"},
	}}); err != nil {
		t.Fatalf("create sqlite automation rule: %v", err)
	}
	if _, err := src.CreateUploadedImage(ctx, store.CreateUploadedImageInput{
		ID:               "img_1",
		OwnerID:          user.ID,
		Filename:         "image.png",
		OriginalFilename: "image.png",
		ContentType:      "image/png",
		Bytes:            123,
		URL:              "/api/uploads/imgs/image.png",
	}); err != nil {
		t.Fatalf("create sqlite image: %v", err)
	}
	if _, err := src.SetStorageUserQuota(ctx, user.ID, 1024); err != nil {
		t.Fatalf("set sqlite quota: %v", err)
	}
	if _, err := src.UpsertStorageFileDeletionFailure(ctx, store.UpsertStorageFileDeletionFailureInput{
		Path:      "/tmp/orphan.png",
		Filename:  "orphan.png",
		OwnerID:   user.ID,
		Bytes:     33,
		LastError: "busy",
	}); err != nil {
		t.Fatalf("create sqlite deletion failure: %v", err)
	}
	conversation, _, err := src.CreatePendingTurn(ctx, store.CreatePendingInput{
		ConversationID: "conv_migrate",
		RequestID:      "req_migrate",
		ResponseID:     "resp_migrate",
		OwnerID:        user.ID,
		RequestFormat:  "responses",
		Model:          "chatapi-lab",
		UserContent:    "hello",
		RequestBody:    map[string]any{"stream": false},
		ToolSchemas:    []any{map[string]any{"name": "tool_a"}},
	})
	if err != nil {
		t.Fatalf("create sqlite pending turn: %v", err)
	}
	if _, _, err := src.CompletePendingTurn(ctx, store.CompletePendingInput{
		ConversationID: conversation.ID,
		ResponseID:     "resp_migrate",
		OutputText:     "world",
		Mode:           "assistant_message",
	}); err != nil {
		t.Fatalf("complete sqlite pending turn: %v", err)
	}

	report, err := migrateDBCommand(ctx, migrateDBOptions{
		command:  "sqlite-to-postgres",
		sqlite:   sqlitePath,
		postgres: dsn,
	})
	if err != nil {
		t.Fatalf("migrate-db command: %v report=%#v", err, report)
	}
	if !report.OK || report.Result.Users != 1 || report.Result.Conversations != 1 || report.Result.Messages != 2 {
		t.Fatalf("unexpected migrate-db report: %#v", report)
	}

	dst, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgres destination store: %v", err)
	}
	t.Cleanup(dst.Close)

	users, err := dst.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("list postgres users: %v len=%d", err, len(users))
	}
	configs, err := dst.ListSystemConfigs(ctx)
	if err != nil || len(configs) != 1 {
		t.Fatalf("list postgres system configs: %v len=%d", err, len(configs))
	}
	conversations, err := dst.ListConversations(ctx)
	if err != nil || len(conversations) != 1 {
		t.Fatalf("list postgres conversations: %v len=%d", err, len(conversations))
	}
	messages, err := dst.ListMessages(ctx, "conv_migrate")
	if err != nil || len(messages) != 2 {
		t.Fatalf("list postgres messages: %v len=%d", err, len(messages))
	}
	appKeys, err := dst.ListAppAPIKeysByUser(ctx, user.ID)
	if err != nil || len(appKeys) != 1 {
		t.Fatalf("list postgres app keys: %v len=%d", err, len(appKeys))
	}
	modelKeys, err := dst.ListModelAPIKeysByUser(ctx, user.ID)
	if err != nil || len(modelKeys) != 1 {
		t.Fatalf("list postgres model keys: %v len=%d", err, len(modelKeys))
	}
	identities, err := dst.ListUserIdentities(ctx, user.ID)
	if err != nil || len(identities) != 1 {
		t.Fatalf("list postgres identities: %v len=%d", err, len(identities))
	}
	images, err := dst.ListUploadedImagesByOwner(ctx, user.ID)
	if err != nil || len(images) != 1 {
		t.Fatalf("list postgres images: %v len=%d", err, len(images))
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("marshal migrate-db report: %v", err)
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
