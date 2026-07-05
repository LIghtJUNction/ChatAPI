package middleware

import (
	"net/http"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

func InjectSessionRequestActor(cfg config.Config) func(http.Handler) http.Handler {
	codec := service.NewSessionCodec(cfg.MasterKey)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Mode == config.ModeLab {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := service.RequestActorFromContext(r.Context()); ok {
				next.ServeHTTP(w, r)
				return
			}
			cookie, err := r.Cookie(service.SessionCookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			actor, err := codec.Decode(cookie.Value)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(service.WithRequestActor(r.Context(), actor)))
		})
	}
}
