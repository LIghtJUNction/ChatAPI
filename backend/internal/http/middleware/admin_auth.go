package middleware

import (
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/service"
)

func RequireAdminActor() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := strings.TrimSpace(r.Header.Get("Authorization"))
			if strings.HasPrefix(strings.ToLower(authz), "bearer ak-") || strings.HasPrefix(strings.ToLower(authz), "bearer sk-") {
				http.Error(w, "admin session required", http.StatusUnauthorized)
				return
			}
			actor, ok := service.RequestActorFromContext(r.Context())
			if !ok || strings.TrimSpace(actor.Role) != "admin" {
				http.Error(w, "admin session required", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
