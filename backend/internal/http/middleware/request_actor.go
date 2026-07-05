package middleware

import (
	"net/http"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

func InjectLabRequestActor(cfg config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Mode != config.ModeLab {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := service.RequestActorFromContext(r.Context()); ok {
				next.ServeHTTP(w, r)
				return
			}
			actor := service.RequestActor{
				UserID:   "lab-user",
				Username: "lab",
				Role:     "admin",
				Source:   "lab",
			}
			next.ServeHTTP(w, r.WithContext(service.WithRequestActor(r.Context(), actor)))
		})
	}
}
