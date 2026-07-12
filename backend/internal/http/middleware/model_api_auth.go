package middleware

import (
	"net/http"

	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"go.uber.org/zap"
)

func RequireModelAPIKey(authService *modelkey.Service, _ *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := authService.AdmitRequest(r.Context(), modelkey.AdmissionInput{
				RawKey: modelkey.ExtractRequestAPIKey(r),
			})
			if result.Decision.Denied() {
				WriteDecisionError(w, result.Decision)
				return
			}
			next.ServeHTTP(w, r.WithContext(result.BindContext(r.Context())))
		})
	}
}
