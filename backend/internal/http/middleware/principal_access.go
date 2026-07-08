package middleware

import (
	"net/http"

	"github.com/zyf/chatapi/internal/ops/observability/logging"
	authaccess "github.com/zyf/chatapi/internal/service/auth/access"
	"go.uber.org/zap"
)

func RequirePrincipalAccess(access *authaccess.Service, baseLogger *zap.Logger) func(http.Handler) http.Handler {
	if access == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := access.PrincipalAdmissionDecision(r.Context())
			if result.Result.Effect == "deny" {
				logging.BindContext(baseLogger, r.Context(),
					zap.String("auth.kind", "principal_access"),
					zap.String("auth.subject", string(result.Result.Subject)),
					zap.String("auth.reason", string(result.Result.Reason)),
					zap.String("auth.error_code", result.Result.ErrorCode),
					zap.String("auth.principal_kind", string(result.Kind)),
					zap.String("auth.principal_key", result.Key),
					zap.String("http.path", r.URL.Path),
				).Warn("principal access rejected")
				WriteDecisionError(w, result.Result)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
