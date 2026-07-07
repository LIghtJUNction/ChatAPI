package middleware

import (
	"net/http"

	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/service/auth/policy"
	"go.uber.org/zap"
)

func RequireAdmin(policyService *policy.Service, baseLogger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := UserSessionPrincipalFromContext(r.Context())
			if !ok || !policyService.IsHumanSession(principal) {
				logging.BindContext(baseLogger, r.Context(),
					zap.String("auth.kind", "user_session"),
					zap.String("http.path", r.URL.Path),
				).Warn("admin session required")
				http.Error(w, "admin session unauthorized", http.StatusUnauthorized)
				return
			}
			if !policyService.IsAdmin(principal) {
				logging.BindContext(baseLogger, r.Context(),
					zap.String("auth.kind", "user_session"),
					zap.String("http.path", r.URL.Path),
					zap.String("user.id", principal.UserID),
				).Warn("admin role required")
				http.Error(w, "admin forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
