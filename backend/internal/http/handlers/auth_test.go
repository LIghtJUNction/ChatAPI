package handlers

import "testing"

func TestMergeOIDCClaimsFillsMissingUserInfoFields(t *testing.T) {
	claims := map[string]any{
		"sub":   "subject-1",
		"email": "id-token@example.com",
	}
	userInfo := map[string]any{
		"sub":                "subject-1",
		"email":              "userinfo@example.com",
		"email_verified":     true,
		"preferred_username": "user-info-name",
	}

	if err := mergeOIDCClaims(claims, userInfo); err != nil {
		t.Fatalf("merge oidc claims: %v", err)
	}
	if claims["email"] != "id-token@example.com" {
		t.Fatalf("userinfo should not overwrite id token email: %#v", claims)
	}
	if claims["email_verified"] != true || claims["preferred_username"] != "user-info-name" {
		t.Fatalf("userinfo should fill missing fields: %#v", claims)
	}
}

func TestMergeOIDCClaimsRejectsSubjectMismatch(t *testing.T) {
	claims := map[string]any{"sub": "id-token-sub"}
	userInfo := map[string]any{"sub": "userinfo-sub"}

	if err := mergeOIDCClaims(claims, userInfo); err == nil {
		t.Fatalf("expected subject mismatch error")
	}
}
