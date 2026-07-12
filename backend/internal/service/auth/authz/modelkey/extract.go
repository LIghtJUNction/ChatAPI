package model

import (
	"net/http"
	"strings"
)

func ExtractRequestAPIKey(r *http.Request) string {
	if r == nil {
		return ""
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	if raw := strings.TrimSpace(r.Header.Get("x-api-key")); raw != "" {
		return raw
	}
	return ""
}
