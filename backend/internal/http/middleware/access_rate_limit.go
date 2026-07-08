package middleware

import (
	"net/http"

	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
)

func RequireAccessRateLimit(access *authaccess.Service) func(http.Handler) http.Handler {
	if access == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !access.AllowRequest(r) {
				http.Error(w, "request rate limited", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
