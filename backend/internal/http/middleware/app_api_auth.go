package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/service"
)

type appAPIPrincipalContextKey struct{}

func RequireAppAPIKey(authService *service.AppAPIKeyService, scopes ...string) func(http.Handler) http.Handler {
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
