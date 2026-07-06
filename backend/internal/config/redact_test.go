package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactedConfigHidesSecrets(t *testing.T) {
	cfg := Default(ModeServe, t.TempDir())
	cfg.MasterKey = "super-secret-master"
	cfg.SessionSecret = "super-secret-session"
	cfg.LabToken = "lab-token-secret"
	cfg.LabPassword = "lab-password-secret"
	cfg.AdminPassword = "admin-password-secret"
	cfg.OIDCClientSecret = "oidc-client-secret"
	cfg.SMTPPassword = "smtp-password-secret"

	data, err := json.Marshal(cfg.Redacted())
	if err != nil {
		t.Fatalf("marshal redacted config: %v", err)
	}
	raw := string(data)
	for _, secret := range []string{
		cfg.MasterKey,
		cfg.SessionSecret,
		cfg.LabToken,
		cfg.LabPassword,
		cfg.AdminPassword,
		cfg.OIDCClientSecret,
		cfg.SMTPPassword,
	} {
		if strings.Contains(raw, secret) {
			t.Fatalf("redacted config leaked secret %q in %s", secret, raw)
		}
	}
	if got := cfg.Redacted().MasterKey; got != "<redacted>" {
		t.Fatalf("unexpected master key redaction: %q", got)
	}
	if got := cfg.Redacted().SessionSecret; got != "<redacted>" {
		t.Fatalf("unexpected session secret redaction: %q", got)
	}
}

func TestRedactedConfigHidesNonSQLiteDSN(t *testing.T) {
	cfg := Default(ModeServe, t.TempDir())
	cfg.DatabaseDriver = "postgres"
	cfg.DatabaseDSN = "postgres://chatapi:secret-password@db.local:5432/chatapi"

	data, err := json.Marshal(cfg.Redacted())
	if err != nil {
		t.Fatalf("marshal redacted config: %v", err)
	}
	if strings.Contains(string(data), "secret-password") || strings.Contains(string(data), cfg.DatabaseDSN) {
		t.Fatalf("redacted config leaked database dsn: %s", data)
	}
	if got := cfg.Redacted().DatabaseDSN; got != "<redacted>" {
		t.Fatalf("unexpected database dsn redaction: %q", got)
	}
}

func TestRedactedConfigKeepsSQLiteDSN(t *testing.T) {
	cfg := Default(ModeLab, t.TempDir())

	redacted := cfg.Redacted()
	if redacted.DatabaseDSN != cfg.DatabaseDSN {
		t.Fatalf("sqlite dsn should stay visible: got %q want %q", redacted.DatabaseDSN, cfg.DatabaseDSN)
	}
}
