package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/http/handlers"
	"github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/store"
)

func NewRouter(cfg config.Config, dataStore store.Store) http.Handler {
	router := chi.NewRouter()
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	router.Use(middleware.RequireLabAccess(cfg))

	healthHandler := handlers.HealthHandler{Config: cfg, Store: dataStore}
	authHandler := handlers.AuthHandler{Config: cfg}
	labHandler := handlers.LabHandler{Config: cfg, Store: dataStore}

	router.Get("/api/health", healthHandler.ServeHTTP)
	router.Get("/api/auth/session", authHandler.Session)
	router.Post("/api/auth/login", authHandler.Login)
	router.Post("/api/auth/logout", authHandler.Logout)
	router.Get("/api/lab/workspace", labHandler.Workspace)
	router.Get("/api/ws", labHandler.WSInfo)
	router.Post("/responses", labHandler.VirtualModel)
	router.Post("/v1/responses", labHandler.VirtualModel)
	router.Post("/chat/completions", labHandler.VirtualModel)
	router.Post("/v1/chat/completions", labHandler.VirtualModel)
	router.Post("/messages", labHandler.VirtualModel)
	router.Post("/v1/messages", labHandler.VirtualModel)

	router.Get("/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "chatapi-lab", "object": "model", "created": 0, "owned_by": "chatapi"},
			},
		})
	})
	router.Get("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/models", http.StatusTemporaryRedirect)
	})

	if info, err := os.Stat(cfg.WebDistDir); err == nil && info.IsDir() {
		fs := http.FileServer(http.Dir(cfg.WebDistDir))
		router.Handle("/*", spaFallback(fs, cfg.WebDistDir))
	}

	return router
}

func spaFallback(next http.Handler, webDistDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(webDistDir, filepath.Clean(r.URL.Path))
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			next.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDistDir, "index.html"))
	})
}
