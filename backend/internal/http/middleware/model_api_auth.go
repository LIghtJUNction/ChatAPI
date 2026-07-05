package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

type modelAPIPrincipalContextKey struct{}

func RequireModelAPIKey(cfg config.Config, authService *service.ModelAPIKeyService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey := extractBearerKey(r)
			if strings.TrimSpace(rawKey) == "" && cfg.Mode == config.ModeLab {
				next.ServeHTTP(w, r)
				return
			}
			principal, err := authService.Authenticate(r.Context(), rawKey)
			if err != nil {
				http.Error(w, "model api key unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), modelAPIPrincipalContextKey{}, principal)
			ctx = service.WithRequestActor(ctx, service.RequestActor{
				UserID:   principal.UserID,
				Username: principal.Name,
				Role:     "model_api",
				Source:   "model_api_key",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ModelAPIPrincipalFromContext(ctx context.Context) (service.ModelAPIPrincipal, bool) {
	principal, ok := ctx.Value(modelAPIPrincipalContextKey{}).(service.ModelAPIPrincipal)
	return principal, ok
}

func extractBearerKey(r *http.Request) string {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	return ""
}
