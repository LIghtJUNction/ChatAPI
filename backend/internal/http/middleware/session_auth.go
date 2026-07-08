package middleware

import (
	"net/http"

	"github.com/zyf/chatapi/internal/ops/observability/logging"
	sessionrestore "github.com/zyf/chatapi/internal/service/auth/authn/sessionrestore"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	"github.com/zyf/chatapi/internal/service/auth/authz/session"
	"go.uber.org/zap"
)

func LoadUserSession(restorer *sessionrestore.Service, baseLogger *zap.Logger) func(http.Handler) http.Handler {
	if restorer == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := restorer.Restore(r)
			if result.Err != nil {
				if result.ClearCookie && restorer.Sessions != nil {
					restorer.Sessions.ClearCookie(w)
					logging.BindContext(baseLogger, r.Context(),
						zap.String("auth.kind", "user_session"),
						zap.String("http.path", r.URL.Path),
					).Warn("user session rejected", zap.Error(result.Err))
				}
				next.ServeHTTP(w, r)
				return
			}
			if !result.Found {
				next.ServeHTTP(w, r)
				return
			}
			ctx := result.BindContext(r.Context())
			logging.BindContext(baseLogger, ctx,
				zap.String("auth.kind", "user_session"),
				zap.String("http.path", r.URL.Path),
				zap.String("user.id", result.Principal.UserID),
			).Debug("user session restored")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireUserSession(policyService *policy.Service, baseLogger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := session.RequireDecision(r.Context(), policyService)
			if result.Denied() {
				logging.BindContext(baseLogger, r.Context(),
					zap.String("auth.kind", "user_session"),
					zap.String("auth.subject", string(result.Subject)),
					zap.String("auth.reason", string(result.Reason)),
					zap.String("auth.error_code", result.ErrorCode),
					zap.String("http.path", r.URL.Path),
				).Warn("user session required")
				WriteDecisionError(w, result)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
