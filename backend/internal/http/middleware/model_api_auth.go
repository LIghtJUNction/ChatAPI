package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/actor"
	modelkey "github.com/zyf/chatapi/internal/apikey/model"
	"github.com/zyf/chatapi/internal/observability/logging"
	"go.uber.org/zap"
)

type modelAPIPrincipalContextKey struct{}

func RequireModelAPIKey(authService *modelkey.Service, baseLogger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey := extractBearerKey(r)
			principal, err := authService.Authenticate(r.Context(), rawKey)
			if err != nil {
				logging.BindContext(baseLogger, r.Context(),
					zap.String("auth.kind", "model_api_key"),
					zap.String("http.path", r.URL.Path),
				).Warn("model api key authentication failed")
				http.Error(w, "model api key unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), modelAPIPrincipalContextKey{}, principal)
			ctx = actor.WithActor(ctx, actor.Actor{
				UserID:      principal.UserID,
				Username:    principal.Name,
				Role:        "model_api",
				Source:      "model_api_key",
				PrincipalID: principal.KeyID,
				EntryPoint:  "virtual_model",
			})
			logging.BindContext(baseLogger, ctx,
				zap.String("auth.kind", "model_api_key"),
				zap.String("http.path", r.URL.Path),
				zap.String("model", principal.Model),
			).Info("model api key authenticated")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ModelAPIPrincipalFromContext(ctx context.Context) (modelkey.Principal, bool) {
	principal, ok := ctx.Value(modelAPIPrincipalContextKey{}).(modelkey.Principal)
	return principal, ok
}

func extractBearerKey(r *http.Request) string {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	if raw := strings.TrimSpace(r.Header.Get("x-api-key")); raw != "" {
		return raw
	}
	return ""
}
