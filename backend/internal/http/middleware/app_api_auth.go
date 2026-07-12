package middleware

import (
	"net/http"

	appkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	"go.uber.org/zap"
)

func RequireAppAPIKey(authService *appkey.Service, _ any, trustedProxies []string, _ *zap.Logger, scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := authService.AdmitRequest(r.Context(), appkey.AdmissionInput{
				RawKey:         appkey.ExtractRequestAPIKey(r),
				SourceIP:       appkey.RequestSourceIP(r, trustedProxies),
				Route:          r.URL.Path,
				RequiredScopes: scopes,
			})
			if result.Decision.Denied() {
				WriteDecisionError(w, result.Decision)
				return
			}
			next.ServeHTTP(w, r.WithContext(result.BindContext(r.Context())))
		})
	}
}
