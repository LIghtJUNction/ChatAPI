package middleware

import (
	"net/http"

	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	authaccess "github.com/zyf/chatapi/internal/service/auth/access"
	"go.uber.org/zap"
)

func RequireLabAccess(access *authaccess.Service, baseLogger *zap.Logger) func(http.Handler) http.Handler {
	if access == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch decision := access.EvaluateLabAccess(r); decision.Kind {
			case authaccess.LabDecisionAllow:
				next.ServeHTTP(w, r)
			case authaccess.LabDecisionGrant:
				access.ApplyLabGrant(w, r)
				if decision.RedirectTo != "" {
					http.Redirect(w, r, decision.RedirectTo, http.StatusSeeOther)
					return
				}
				next.ServeHTTP(w, r)
			case authaccess.LabDecisionRender:
				access.Lab().RenderPasswordPage(w, r)
			default:
				logging.BindContext(baseLogger, r.Context(),
					zap.String("auth.kind", "lab_access"),
					zap.String("http.path", r.URL.Path),
				).Warn("lab access denied")
				http.Error(w, "lab access denied", http.StatusUnauthorized)
			}
		})
	}
}

func LoadLabActor(access *authaccess.Service, baseLogger *zap.Logger) func(http.Handler) http.Handler {
	if access == nil || access.Lab() == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !access.ShouldInjectLabActor(r) {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := actor.FromContext(r.Context()); ok {
				next.ServeHTTP(w, r)
				return
			}
			ctx := actor.WithActor(r.Context(), access.Lab().Actor())
			logging.BindContext(baseLogger, ctx,
				zap.String("auth.kind", "lab"),
				zap.String("http.path", r.URL.Path),
			).Debug("lab actor injected")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
