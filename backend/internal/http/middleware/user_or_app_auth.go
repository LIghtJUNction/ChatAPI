package middleware

import "net/http"

func RequireUserSessionOrAppAPI(appAuth func(http.Handler) http.Handler, requireSession func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := UserSessionPrincipalFromContext(r.Context()); ok {
				requireSession(next).ServeHTTP(w, r)
				return
			}
			appAuth(next).ServeHTTP(w, r)
		})
	}
}
