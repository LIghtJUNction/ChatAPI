package middleware

import (
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/config"
)

func RequireLabAccess(cfg config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Mode != config.ModeLab {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.LabPassword == "" && cfg.LabToken == "" {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.LabPassword != "" {
				password := strings.TrimSpace(r.URL.Query().Get("password"))
				if password == cfg.LabPassword {
					next.ServeHTTP(w, r)
					return
				}
			}

			if cfg.LabToken != "" {
				token := strings.TrimSpace(r.URL.Query().Get("token"))
				if token == cfg.LabToken {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, "lab access denied", http.StatusUnauthorized)
		})
	}
}
