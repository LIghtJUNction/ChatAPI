package middleware

import (
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/service"
)

func RequireUserActor() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, ok := service.RequestActorFromContext(r.Context())
			if !ok || !service.IsInteractiveUserActor(actor) {
				http.Error(w, "session required", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func CurrentUserID(r *http.Request) string {
	if r == nil {
		return ""
	}
	actor, ok := service.RequestActorFromContext(r.Context())
	if !ok || !service.IsInteractiveUserActor(actor) {
		return ""
	}
	return strings.TrimSpace(actor.UserID)
}
