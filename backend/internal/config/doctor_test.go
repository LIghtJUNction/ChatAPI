package config

import (
	"testing"
)

func TestDiagnoseServeRequiresProductionSecrets(t *testing.T) {
	cfg := Default(ModeServe, t.TempDir())
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

func TestDiagnoseOIDCPrivateRPRequirements(t *testing.T) {
	cfg := Default(ModeServe, t.TempDir())
	cfg.MasterKey = "01234567890123456789012345678901"
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

func hasDiagnostic(report DiagnosticReport, severity string, code string) bool {
	for _, item := range report.Items {
		if item.Severity == severity && item.Code == code {
			return true
		}
	}
	return false
}
