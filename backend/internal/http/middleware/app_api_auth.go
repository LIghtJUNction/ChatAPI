package middleware

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/actor"
	appkey "github.com/zyf/chatapi/internal/apikey/app"
	"github.com/zyf/chatapi/internal/observability/logging"
	"go.uber.org/zap"
)

type appAPIPrincipalContextKey struct{}

func RequireAppAPIKey(authService *appkey.Service, trustedProxies []string, baseLogger *zap.Logger, scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey := extractAppAPIKey(r)
			principal, err := authService.Authenticate(r.Context(), rawKey)
			if err != nil {
				logging.BindContext(baseLogger, r.Context(),
					zap.String("auth.kind", "app_api_key"),
					zap.String("http.path", r.URL.Path),
				).Warn("app api key authentication failed")
				http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), appAPIPrincipalContextKey{}, principal)
			ctx = actor.WithActor(ctx, actor.Actor{
				UserID:      principal.UserID,
				Username:    principal.Name,
				Role:        "app_api",
				Source:      "app_api_key",
				PrincipalID: principal.KeyID,
				EntryPoint:  "app_api",
			})
			sourceIP := appAPIRequestSourceIP(r, trustedProxies)
			if !authService.AllowSourceIP(principal, sourceIP) {
				logging.BindContext(baseLogger, ctx,
					zap.String("auth.kind", "app_api_key"),
					zap.String("auth.decision", "source_ip_forbidden"),
					zap.String("http.path", r.URL.Path),
					zap.String("source_ip", sourceIP),
				).Warn("app api key source ip rejected")
				authService.RecordAudit(ctx, principal, r.URL.Path, http.StatusForbidden, "source_ip_forbidden")
				http.Error(w, "app api key source ip forbidden", http.StatusForbidden)
				return
			}
			for _, scope := range scopes {
				if _, ok := principal.Scopes[scope]; !ok {
					logging.BindContext(baseLogger, ctx,
						zap.String("auth.kind", "app_api_key"),
						zap.String("auth.decision", "scope_forbidden"),
						zap.String("http.path", r.URL.Path),
						zap.String("required.scope", scope),
					).Warn("app api key scope rejected")
					authService.RecordAudit(ctx, principal, r.URL.Path, http.StatusForbidden, "forbidden")
					http.Error(w, "app api key forbidden", http.StatusForbidden)
					return
				}
			}
			if !authService.AllowRequest(principal, time.Now().UTC()) {
				logging.BindContext(baseLogger, ctx,
					zap.String("auth.kind", "app_api_key"),
					zap.String("auth.decision", "rate_limited"),
					zap.String("http.path", r.URL.Path),
				).Warn("app api key rate limited")
				authService.RecordAudit(ctx, principal, r.URL.Path, http.StatusTooManyRequests, "rate_limited")
				http.Error(w, "app api key rate limited", http.StatusTooManyRequests)
				return
			}
			logging.BindContext(baseLogger, ctx,
				zap.String("auth.kind", "app_api_key"),
				zap.String("http.path", r.URL.Path),
				zap.Strings("auth.required_scopes", scopes),
			).Info("app api key authenticated")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func AppAPIPrincipalFromContext(ctx context.Context) (appkey.Principal, bool) {
	principal, ok := ctx.Value(appAPIPrincipalContextKey{}).(appkey.Principal)
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
