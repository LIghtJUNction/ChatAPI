package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/service/auth/authz/principal"
	"github.com/zyf/chatapi/internal/service/auth/authz/session"
	"go.uber.org/zap"
)

type userSessionPrincipalContextKey struct{}

func LoadUserSession(sessionService *session.Service, baseLogger *zap.Logger) func(http.Handler) http.Handler {
	if sessionService == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pr, claims, err := sessionService.PrincipalFromRequest(r)
			if err != nil {
				if !errors.Is(err, session.ErrMissingCookie) {
					sessionService.ClearCookie(w)
					logging.BindContext(baseLogger, r.Context(),
						zap.String("auth.kind", "user_session"),
						zap.String("http.path", r.URL.Path),
					).Warn("user session rejected", zap.Error(err))
				}
				next.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), userSessionPrincipalContextKey{}, pr)
			ctx = session.ContextWithClaims(ctx, claims)
			ctx = actor.WithActor(ctx, pr.Actor())
			logging.BindContext(baseLogger, ctx,
				zap.String("auth.kind", "user_session"),
				zap.String("http.path", r.URL.Path),
				zap.String("user.id", pr.UserID),
			).Debug("user session restored")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireUserSession(baseLogger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := UserSessionPrincipalFromContext(r.Context()); !ok {
				logging.BindContext(baseLogger, r.Context(),
					zap.String("auth.kind", "user_session"),
					zap.String("http.path", r.URL.Path),
				).Warn("user session required")
				http.Error(w, "session unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func UserSessionPrincipalFromContext(ctx context.Context) (principal.Principal, bool) {
	pr, ok := ctx.Value(userSessionPrincipalContextKey{}).(principal.Principal)
	return pr, ok
}
