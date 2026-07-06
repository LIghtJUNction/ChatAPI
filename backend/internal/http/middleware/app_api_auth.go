package middleware

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/service"
)

type appAPIPrincipalContextKey struct{}

func RequireAppAPIKey(authService *service.AppAPIKeyService, trustedProxies []string, scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey := extractAppAPIKey(r)
			principal, err := authService.Authenticate(r.Context(), rawKey)
			if err != nil {
				http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), appAPIPrincipalContextKey{}, principal)
			ctx = service.WithRequestActor(ctx, service.RequestActor{
				UserID:   principal.UserID,
				Username: principal.Name,
				Role:     "app_api",
				Source:   "app_api_key",
			})
			sourceIP := appAPIRequestSourceIP(r, trustedProxies)
			if !authService.AllowSourceIP(principal, sourceIP) {
				authService.RecordAudit(ctx, principal, r.URL.Path, http.StatusForbidden, "source_ip_forbidden")
				http.Error(w, "app api key source ip forbidden", http.StatusForbidden)
				return
			}
			for _, scope := range scopes {
				if _, ok := principal.Scopes[scope]; !ok {
					authService.RecordAudit(ctx, principal, r.URL.Path, http.StatusForbidden, "forbidden")
					http.Error(w, "app api key forbidden", http.StatusForbidden)
					return
				}
			}
			if !authService.AllowRequest(principal, time.Now().UTC()) {
				authService.RecordAudit(ctx, principal, r.URL.Path, http.StatusTooManyRequests, "rate_limited")
				http.Error(w, "app api key rate limited", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func appAPIRequestSourceIP(r *http.Request, trustedProxies []string) string {
	remoteIP := hostFromRemoteAddr(r.RemoteAddr)
	if !isTrustedProxy(remoteIP, trustedProxies) {
		return remoteIP
	}
	if value := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); value != "" {
		parts := strings.Split(value, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				return part
			}
		}
	}
	if value := strings.TrimSpace(r.Header.Get("X-Real-IP")); value != "" {
		return value
	}
	return remoteIP
}

func hostFromRemoteAddr(remoteAddr string) string {
	host := strings.TrimSpace(remoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	return host
}

func isTrustedProxy(remoteIP string, trustedProxies []string) bool {
	if len(trustedProxies) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(remoteIP))
	if err != nil {
		return false
	}
	for _, rawRule := range trustedProxies {
		rule := strings.TrimSpace(rawRule)
		if rule == "" {
			continue
		}
		if strings.Contains(rule, "/") {
			prefix, err := netip.ParsePrefix(rule)
			if err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		allowedAddr, err := netip.ParseAddr(rule)
		if err == nil && allowedAddr == addr {
			return true
		}
	}
	return false
}

func AppAPIPrincipalFromContext(ctx context.Context) (service.AppAPIPrincipal, bool) {
	principal, ok := ctx.Value(appAPIPrincipalContextKey{}).(service.AppAPIPrincipal)
	return principal, ok
}

func extractAppAPIKey(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-ChatAPI-App-Key")); value != "" {
		return value
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	return ""
}
