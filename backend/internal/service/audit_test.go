package service

import "testing"

func TestSanitizeAuditMetadataDropsSensitiveKeys(t *testing.T) {
	result := sanitizeAuditMetadata(map[string]any{
		"filename":      "demo.png",
		"api_key":       "secret-key",
		"access_token":  "secret-token",
		"password":      "secret-password",
		"client_secret": "secret-client",
	})
	if result["filename"] != "demo.png" {
		t.Fatalf("expected safe metadata to remain: %#v", result)
	}
	for _, key := range []string{"api_key", "access_token", "password", "client_secret"} {
		if _, ok := result[key]; ok {
			t.Fatalf("sensitive metadata key %q was not removed: %#v", key, result)
		}
	}
}
