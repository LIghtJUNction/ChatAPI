package config

import (
	"testing"
	"time"
)

func TestDiagnoseServeRequiresProductionSecrets(t *testing.T) {
	cfg := Default(ModeServe, t.TempDir())
	cfg.AdminUsername = "root"
	cfg.AdminPassword = "change-me"

	report := Diagnose(cfg, cfg.Validate())
	if report.OK {
		t.Fatalf("expected serve diagnostics to fail: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "secret.master_key_missing") {
		t.Fatalf("missing master key diagnostic: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "secret.admin_password_default") {
		t.Fatalf("missing default admin password diagnostic: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticInfo, "secret.session_secret_generated") {
		t.Fatalf("missing generated session secret diagnostic: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticWarn, "database.sqlite_in_serve") {
		t.Fatalf("missing sqlite serve warning: %#v", report)
	}
}

func TestDiagnoseLabDefaultIsAllowed(t *testing.T) {
	cfg := Default(ModeLab, t.TempDir())

	report := Diagnose(cfg, cfg.Validate())
	if !report.OK {
		t.Fatalf("expected local lab diagnostics to pass: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticInfo, "mode.lab") {
		t.Fatalf("missing lab mode info: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticInfo, "secret.lab_master_key") {
		t.Fatalf("missing lab master key info: %#v", report)
	}
}

func TestFromEnvUncheckedKeepsLabDefaultMasterKey(t *testing.T) {
	t.Setenv("CHATAPI_MASTER_KEY", "")

	cfg, err := FromEnvUnchecked(ModeLab, t.TempDir())
	if err != nil {
		t.Fatalf("load lab config: %v", err)
	}
	if cfg.MasterKey != "chatapi-lab-insecure-master-key" {
		t.Fatalf("unexpected lab master key: %q", cfg.MasterKey)
	}
}

func TestFromEnvLoadsTrustedProxies(t *testing.T) {
	t.Setenv("CHATAPI_TRUSTED_PROXIES", "127.0.0.1,10.0.0.0/8")

	cfg, err := FromEnvUnchecked(ModeServe, t.TempDir())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "127.0.0.1" || cfg.TrustedProxies[1] != "10.0.0.0/8" {
		t.Fatalf("unexpected trusted proxies: %#v", cfg.TrustedProxies)
	}
}

func TestFromEnvLoadsSessionSecret(t *testing.T) {
	t.Setenv("CHATAPI_SESSION_SECRET", "session-secret-from-env")

	cfg, err := FromEnvUnchecked(ModeServe, t.TempDir())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.SessionSecret != "session-secret-from-env" {
		t.Fatalf("unexpected session secret: %q", cfg.SessionSecret)
	}
}

func TestDefaultLogHTTPSummaryEnabledByMode(t *testing.T) {
	serveCfg := Default(ModeServe, t.TempDir())
	if serveCfg.LogHTTPSummaryEnabled {
		t.Fatal("expected serve mode http summary logging to default to disabled")
	}

	labCfg := Default(ModeLab, t.TempDir())
	if !labCfg.LogHTTPSummaryEnabled {
		t.Fatal("expected lab mode http summary logging to default to enabled")
	}
}

func TestFromEnvLoadsLogHTTPSummaryEnabled(t *testing.T) {
	t.Setenv("CHATAPI_LOG_HTTP_SUMMARY_ENABLED", "1")

	cfg, err := FromEnvUnchecked(ModeServe, t.TempDir())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.LogHTTPSummaryEnabled {
		t.Fatal("expected http summary logging to be enabled")
	}
}

func TestDiagnoseTrustedProxyValidation(t *testing.T) {
	cfg := Default(ModeServe, t.TempDir())
	cfg.MasterKey = "01234567890123456789012345678901"
	cfg.SessionSecret = "01234567890123456789012345678901"
	cfg.AdminUsername = "root"
	cfg.AdminPassword = "not-change-me"
	cfg.TrustedProxies = []string{"not-an-ip"}

	report := Diagnose(cfg, cfg.Validate())
	if report.OK {
		t.Fatalf("expected trusted proxy diagnostics to fail: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "trusted_proxy.invalid") {
		t.Fatalf("missing trusted proxy diagnostic: %#v", report)
	}
}

func TestDiagnoseWarnsForShortSessionSecret(t *testing.T) {
	cfg := Default(ModeServe, t.TempDir())
	cfg.MasterKey = "01234567890123456789012345678901"
	cfg.SessionSecret = "short"
	cfg.AdminUsername = "root"
	cfg.AdminPassword = "not-change-me"

	report := Diagnose(cfg, cfg.Validate())
	if !hasDiagnostic(report, DiagnosticWarn, "secret.session_secret_short") {
		t.Fatalf("missing short session secret warning: %#v", report)
	}
}

func TestDiagnoseOIDCPrivateRPRequirements(t *testing.T) {
	cfg := Default(ModeServe, t.TempDir())
	cfg.MasterKey = "01234567890123456789012345678901"
	cfg.SessionSecret = "01234567890123456789012345678901"
	cfg.AdminUsername = "root"
	cfg.AdminPassword = "not-change-me"
	cfg.OIDCEnabled = true
	cfg.OIDCClientID = "chatapi"
	cfg.OIDCScopes = []string{"email"}
	cfg.OIDCRedirectURL = "http://chat.example.com/api/auth/oidc/callback"

	report := Diagnose(cfg, cfg.Validate())
	if report.OK {
		t.Fatalf("expected OIDC diagnostics to fail: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "oidc.issuer_missing") {
		t.Fatalf("missing issuer diagnostic: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "oidc.client_secret_missing") {
		t.Fatalf("missing client secret diagnostic: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "oidc.redirect_url_invalid") {
		t.Fatalf("missing redirect URL diagnostic: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "oidc.scope_openid_missing") {
		t.Fatalf("missing openid scope diagnostic: %#v", report)
	}
}

func TestFromEnvLoadsSMTPConfig(t *testing.T) {
	t.Setenv("CHATAPI_SMTP_ENABLED", "1")
	t.Setenv("CHATAPI_SMTP_HOST", "smtp.example.com")
	t.Setenv("CHATAPI_SMTP_PORT", "465")
	t.Setenv("CHATAPI_SMTP_USERNAME", "chatapi")
	t.Setenv("CHATAPI_SMTP_PASSWORD", "smtp-secret")
	t.Setenv("CHATAPI_SMTP_FROM", "ChatAPI <noreply@example.com>")
	t.Setenv("CHATAPI_SMTP_SECURITY", "tls")
	t.Setenv("CHATAPI_SMTP_TIMEOUT", "3s")

	cfg, err := FromEnvUnchecked(ModeLab, t.TempDir())
	if err != nil {
		t.Fatalf("load smtp config: %v", err)
	}
	if !cfg.SMTPEnabled || cfg.SMTPHost != "smtp.example.com" || cfg.SMTPPort != 465 || cfg.SMTPUsername != "chatapi" || cfg.SMTPPassword != "smtp-secret" {
		t.Fatalf("unexpected smtp config: %#v", cfg)
	}
	if cfg.SMTPFrom != "ChatAPI <noreply@example.com>" || cfg.SMTPSecurity != "tls" || cfg.SMTPTimeout != 3*time.Second {
		t.Fatalf("unexpected smtp config details: %#v", cfg)
	}
}

func TestFromEnvLoadsGeeTestConfig(t *testing.T) {
	t.Setenv("CHATAPI_GEETEST_CAPTCHA_ID", "captcha-id")
	t.Setenv("CHATAPI_GEETEST_CAPTCHA_KEY", "captcha-key")
	t.Setenv("CHATAPI_GEETEST_API_SERVER", "https://geetest.example.com")

	cfg, err := FromEnvUnchecked(ModeLab, t.TempDir())
	if err != nil {
		t.Fatalf("load geetest config: %v", err)
	}
	if cfg.GeetestCaptchaID != "captcha-id" || cfg.GeetestCaptchaKey != "captcha-key" || cfg.GeetestAPIServer != "https://geetest.example.com" {
		t.Fatalf("unexpected geetest config: %#v", cfg)
	}
}

func TestFromEnvLoadsStorageQuota(t *testing.T) {
	t.Setenv("CHATAPI_STORAGE_DEFAULT_QUOTA_BYTES", "12345")

	cfg, err := FromEnvUnchecked(ModeLab, t.TempDir())
	if err != nil {
		t.Fatalf("load storage quota config: %v", err)
	}
	if cfg.StorageDefaultQuotaBytes != 12345 {
		t.Fatalf("unexpected storage quota: %d", cfg.StorageDefaultQuotaBytes)
	}
}

func TestFromEnvLoadsStorageBlockNewConversations(t *testing.T) {
	t.Setenv("CHATAPI_STORAGE_BLOCK_NEW_CONVERSATIONS", "1")

	cfg, err := FromEnvUnchecked(ModeLab, t.TempDir())
	if err != nil {
		t.Fatalf("load storage block new conversations config: %v", err)
	}
	if !cfg.StorageBlockNewConversations {
		t.Fatalf("expected storage block new conversations to be enabled: %#v", cfg)
	}
}

func TestFromEnvLoadsStorageCleanupConfig(t *testing.T) {
	t.Setenv("CHATAPI_STORAGE_CLEANUP_ENABLED", "1")
	t.Setenv("CHATAPI_STORAGE_CLEANUP_TIME", "04:30")
	t.Setenv("CHATAPI_STORAGE_CLEANUP_KEEP_RECENT_CONVERSATIONS", "20")
	t.Setenv("CHATAPI_STORAGE_CLEANUP_KEEP_RECENT_DAYS", "14")
	t.Setenv("CHATAPI_STORAGE_VACUUM_ENABLED", "1")

	cfg, err := FromEnvUnchecked(ModeLab, t.TempDir())
	if err != nil {
		t.Fatalf("load storage cleanup config: %v", err)
	}
	if !cfg.StorageCleanupEnabled || cfg.StorageCleanupTime != "04:30" || cfg.StorageCleanupKeepRecentConversations != 20 || cfg.StorageCleanupKeepRecentDays != 14 || !cfg.StorageVacuumEnabled {
		t.Fatalf("unexpected storage cleanup config: %#v", cfg)
	}
}

func TestFromEnvLoadsRuntimeSettings(t *testing.T) {
	t.Setenv("CHATAPI_RUNTIME_GOGC", "75")
	t.Setenv("CHATAPI_RUNTIME_MEMORY_LIMIT_BYTES", "268435456")

	cfg, err := FromEnvUnchecked(ModeLab, t.TempDir())
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if cfg.RuntimeGOGC != 75 || cfg.RuntimeMemoryLimitBytes != 268435456 {
		t.Fatalf("unexpected runtime config: %#v", cfg)
	}
}

func TestFromEnvLoadsRealtimeLimits(t *testing.T) {
	t.Setenv("CHATAPI_REALTIME_MAX_CONNECTIONS", "100")
	t.Setenv("CHATAPI_REALTIME_MAX_CONNECTIONS_PER_USER", "8")
	t.Setenv("CHATAPI_REALTIME_WEBUI_RESERVED_PER_USER", "2")

	cfg, err := FromEnvUnchecked(ModeLab, t.TempDir())
	if err != nil {
		t.Fatalf("load realtime config: %v", err)
	}
	if cfg.RealtimeMaxConnections != 100 || cfg.RealtimeMaxConnectionsPerUser != 8 || cfg.RealtimeWebUIReservedPerUser != 2 {
		t.Fatalf("unexpected realtime config: %#v", cfg)
	}
}

func TestDiagnoseStorageQuota(t *testing.T) {
	cfg := Default(ModeLab, t.TempDir())

	report := Diagnose(cfg, cfg.Validate())
	if !hasDiagnostic(report, DiagnosticInfo, "storage.quota_disabled") {
		t.Fatalf("missing storage quota disabled diagnostic: %#v", report)
	}

	cfg.StorageDefaultQuotaBytes = 1024
	report = Diagnose(cfg, cfg.Validate())
	if !hasDiagnostic(report, DiagnosticWarn, "storage.quota_low") {
		t.Fatalf("missing low storage quota diagnostic: %#v", report)
	}
}

func TestDiagnoseGeeTestPartialConfig(t *testing.T) {
	cfg := Default(ModeServe, t.TempDir())
	cfg.MasterKey = "01234567890123456789012345678901"
	cfg.SessionSecret = "01234567890123456789012345678901"
	cfg.AdminPassword = "not-change-me"
	cfg.GeetestCaptchaID = "captcha-id"

	report := Diagnose(cfg, cfg.Validate())
	if report.OK {
		t.Fatalf("expected geetest diagnostics to fail: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "geetest.partial_config") {
		t.Fatalf("missing geetest partial config diagnostic: %#v", report)
	}
}

func TestDiagnoseStorageCleanup(t *testing.T) {
	cfg := Default(ModeLab, t.TempDir())
	cfg.StorageCleanupEnabled = true
	cfg.StorageCleanupKeepRecentConversations = 0
	cfg.StorageCleanupKeepRecentDays = 0
	cfg.StorageVacuumEnabled = true

	report := Diagnose(cfg, cfg.Validate())
	if !hasDiagnostic(report, DiagnosticInfo, "storage.cleanup_enabled") {
		t.Fatalf("missing storage cleanup enabled diagnostic: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticWarn, "storage.cleanup_retention_deprecated") {
		t.Fatalf("missing deprecated retention warning: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticWarn, "storage.vacuum_enabled") {
		t.Fatalf("missing vacuum warning: %#v", report)
	}
}

func TestDiagnoseRuntimeSettings(t *testing.T) {
	cfg := Default(ModeLab, t.TempDir())
	cfg.RuntimeGOGC = 10
	cfg.RuntimeMemoryLimitBytes = 32 << 20

	report := Diagnose(cfg, cfg.Validate())
	if !hasDiagnostic(report, DiagnosticWarn, "runtime.gogc_low") {
		t.Fatalf("missing low runtime gogc diagnostic: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticWarn, "runtime.memory_limit_low") {
		t.Fatalf("missing low runtime memory limit diagnostic: %#v", report)
	}
}

func TestDiagnoseRealtimeLimits(t *testing.T) {
	cfg := Default(ModeLab, t.TempDir())

	report := Diagnose(cfg, cfg.Validate())
	if !hasDiagnostic(report, DiagnosticInfo, "realtime.webui_reservation_inactive") {
		t.Fatalf("missing inactive realtime reservation diagnostic: %#v", report)
	}

	cfg.RealtimeMaxConnectionsPerUser = 1
	cfg.RealtimeWebUIReservedPerUser = 1
	report = Diagnose(cfg, cfg.Validate())
	if !hasDiagnostic(report, DiagnosticWarn, "realtime.webui_reserved_too_high") {
		t.Fatalf("missing high realtime reservation diagnostic: %#v", report)
	}
}

func TestDiagnoseSMTPRequirements(t *testing.T) {
	cfg := Default(ModeLab, t.TempDir())
	cfg.SMTPEnabled = true
	cfg.SMTPSecurity = "bad"

	report := Diagnose(cfg, cfg.Validate())
	if report.OK {
		t.Fatalf("expected SMTP diagnostics to fail: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "smtp.host_missing") {
		t.Fatalf("missing smtp host diagnostic: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "smtp.from_missing") {
		t.Fatalf("missing smtp from diagnostic: %#v", report)
	}
	if !hasDiagnostic(report, DiagnosticError, "smtp.security_invalid") {
		t.Fatalf("missing smtp security diagnostic: %#v", report)
	}
}

func hasDiagnostic(report DiagnosticReport, severity string, code string) bool {
	for _, item := range report.Items {
		if item.Severity == severity && item.Code == code {
			return true
		}
	}
	return false
}
