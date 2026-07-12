package middleware

import (
	"net/http"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	"go.uber.org/zap"
)

func RequireSessionCSRF(access *authaccess.Service, policyService *policy.Service, baseLogger *zap.Logger) func(http.Handler) http.Handler {
	if access == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := access.SessionCSRFDecision(r.Context(), r, policyService)
			if result.Denied() {
				logging.BindContext(baseLogger, r.Context(),
					zap.String("auth.kind", "csrf"),
					zap.String("auth.subject", string(result.Subject)),
					zap.String("auth.reason", string(result.Reason)),
					zap.String("auth.error_code", result.ErrorCode),
					zap.String("http.path", r.URL.Path),
				).Warn("session csrf rejected")
				WriteDecisionError(w, result)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
