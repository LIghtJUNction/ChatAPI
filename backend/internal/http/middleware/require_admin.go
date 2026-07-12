package middleware

import (
	"net/http"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	"go.uber.org/zap"
)

func RequireAdmin(policyService *policy.Service, baseLogger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := session.RequireAdminDecision(r.Context(), policyService)
			if result.Denied() {
				logging.BindContext(baseLogger, r.Context(),
					zap.String("auth.kind", "user_session"),
					zap.String("auth.subject", string(result.Subject)),
					zap.String("auth.reason", string(result.Reason)),
					zap.String("auth.error_code", result.ErrorCode),
					zap.String("http.path", r.URL.Path),
				).Warn("admin access rejected")
				WriteDecisionError(w, result)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
