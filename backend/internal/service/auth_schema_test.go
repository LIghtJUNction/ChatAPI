package service

import (
	"testing"

	"github.com/zyf/chatapi/internal/config"
)

func TestBuildAuthSchema(t *testing.T) {
	schema := BuildAuthSchema(config.Config{Mode: config.ModeServe, OIDCEnabled: true, OIDCProviderName: "Kirari"}, AuthPublicSettings{
		RegistrationEnabled:      true,
		EmailVerificationEnabled: true,
		PasswordResetEnabled:     true,
	})
	if len(schema.Operations) != 16 {
		t.Fatalf("unexpected auth schema operations: %#v", schema)
	}
	if schema.Capabilities["oidc_enabled"] != true || schema.Capabilities["oidc_provider_name"] != "Kirari" {
		t.Fatalf("unexpected auth schema capabilities: %#v", schema.Capabilities)
	}
	if schema.Operations[1].Name != "login" || len(schema.Operations[1].Fields) != 4 {
		t.Fatalf("unexpected auth login schema: %#v", schema.Operations[1])
	}
	if schema.Operations[14].Name != "oidc_link" || schema.Operations[15].Name != "oidc_callback" {
		t.Fatalf("unexpected oidc auth schema operations: %#v", schema.Operations)
	}
}
