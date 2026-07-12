package access

import (
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/config"
)

func isLabPublicPath(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	switch r.URL.Path {
	case "/api/health", "/api/ready", "/api/setup/status", "/setup", "/metrics":
		return true
	default:
		return false
	}
}

func requiresCSRFCheck(r *http.Request) bool {
	if r == nil || r.URL == nil || !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func isBearerAPIKeyRequest(r *http.Request) bool {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return false
	}
	token := strings.TrimSpace(authz[7:])
	return strings.HasPrefix(token, "ak-") || strings.HasPrefix(token, "sk-")
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r != nil && r.TLS != nil {
		scheme = "https"
	}
	host := ""
	if r != nil {
		host = strings.ToLower(strings.TrimSpace(r.Host))
	}
	return scheme + "://" + host
}

func buildTrustedOrigins(cfg config.Config) map[string]struct{} {
	items := map[string]struct{}{}
	addTrustedOrigin(items, cfg.BaseURL)
	for _, origin := range cfg.CORSOrigins {
		addTrustedOrigin(items, origin)
	}
	return items
}

func addTrustedOrigin(items map[string]struct{}, raw string) {
	origin := normalizedOrigin(raw)
	if origin == "" {
		return
	}
	items[origin] = struct{}{}
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

func accessRateLimitKey(r *http.Request) string {
	if r == nil {
		return ""
	}
	host := strings.TrimSpace(r.Host)
	remote := normalizeRemoteAddr(strings.TrimSpace(r.RemoteAddr))
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		remote = forwarded
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		remote = realIP
	}
	if remote == "" {
		remote = "unknown"
	}
	return host + "|" + remote
}

func normalizeRemoteAddr(raw string) string {
	if raw == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(raw)
	if err == nil {
		return strings.TrimSpace(host)
	}
	return strings.TrimSpace(raw)
}

func WantsHTML(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept")))
	if strings.Contains(accept, "text/html") {
		return true
	}
	if accept != "" && !strings.Contains(accept, "*/*") {
		return false
	}
	if r.URL == nil {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/lab/") || r.URL.Path == "/messages" {
		return false
	}
	ext := path.Ext(r.URL.Path)
	return ext == "" || ext == ".html"
}
