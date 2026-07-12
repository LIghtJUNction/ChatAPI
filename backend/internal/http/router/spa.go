package router

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/http/webassets"
)

func mountSPA(router chi.Router, cfg config.Config) {
	if assets, ok := webassets.FS(); ok {
		router.Handle("/*", spaFallback(assets))
		return
	}
	if info, err := os.Stat(cfg.WebDistDir); err == nil && info.IsDir() {
		router.Handle("/*", spaFallback(os.DirFS(cfg.WebDistDir)))
	}
}

func spaFallback(assets fs.FS) http.Handler {
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		cleanPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if stat, err := fs.Stat(assets, cleanPath); err == nil && !stat.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		if isReservedBackendPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		index, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodGet {
			_, _ = w.Write(index)
		}
	})
}

func isReservedBackendPath(requestPath string) bool {
	clean := "/" + strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	return clean == "/api" || strings.HasPrefix(clean, "/api/") || clean == "/v1" || strings.HasPrefix(clean, "/v1/")
}
