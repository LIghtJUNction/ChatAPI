package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

func RequireSessionCSRF(cfg config.Config) func(http.Handler) http.Handler {
	baseOrigin := normalizedOrigin(cfg.BaseURL)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Mode == config.ModeLab || !requiresCSRFCheck(r) {
				next.ServeHTTP(w, r)
				return
			}
			if isBearerAPIKeyRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			actor, ok := service.RequestActorFromContext(r.Context())
			if !ok || actor.Source != "session" {
				next.ServeHTTP(w, r)
				return
			}
			if !validCSRFSameOrigin(r, baseOrigin) {
				http.Error(w, "csrf origin check failed", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requiresCSRFCheck(r *http.Request) bool {
	if r == nil || !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func validCSRFSameOrigin(r *http.Request, baseOrigin string) bool {
	requestOrigin := requestOrigin(r)
	origin := normalizedOrigin(r.Header.Get("Origin"))
	if origin == "" {
		origin = normalizedOrigin(r.Header.Get("Referer"))
	}
	if origin == "" {
		return false
	}
	return origin == requestOrigin || (baseOrigin != "" && origin == baseOrigin)
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + strings.ToLower(strings.TrimSpace(r.Host))
}

func normalizedOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

func isBearerAPIKeyRequest(r *http.Request) bool {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return false
	}
	token := strings.TrimSpace(authz[7:])
	return strings.HasPrefix(token, "ak-") || strings.HasPrefix(token, "sk-")
}
