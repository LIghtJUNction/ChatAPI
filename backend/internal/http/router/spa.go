package router

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/zyf2007/ChatAPI/internal/config"
)

func mountSPA(router chi.Router, cfg config.Config) {
	if info, err := os.Stat(cfg.WebDistDir); err == nil && info.IsDir() {
		fs := http.FileServer(http.Dir(cfg.WebDistDir))
		router.Handle("/*", spaFallback(fs, cfg.WebDistDir))
	}
}

func spaFallback(next http.Handler, webDistDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := filepath.Join(webDistDir, filepath.Clean(r.URL.Path))
		if stat, err := os.Stat(target); err == nil && !stat.IsDir() {
			next.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDistDir, "index.html"))
	})
}
