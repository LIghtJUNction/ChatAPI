package middleware

import (
	"context"
	"net/http"
	"strings"

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
			for _, scope := range scopes {
				if _, ok := principal.Scopes[scope]; !ok {
					http.Error(w, "app api key forbidden", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), appAPIPrincipalContextKey{}, principal)))
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
