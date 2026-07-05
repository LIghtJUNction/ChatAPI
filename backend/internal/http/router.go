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
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

func NewRouter(
	cfg config.Config,
	dataStore store.Store,
	chatService *service.ChatAPIService,
	realtimeHub *service.RealtimeHub,
	pending *service.PendingRegistry,
) http.Handler {
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
	labHandler := handlers.LabHandler{Config: cfg, Store: dataStore, Service: chatService}
	appAPIKeyService := service.NewAppAPIKeyService(dataStore)
	appAPIHandler := handlers.AppAPIHandler{Service: chatService}
	userAppAPIKeysHandler := handlers.UserAppAPIKeysHandler{Config: cfg, AppAPIKeys: appAPIKeyService}
	chatHandler := handlers.ChatAPIHandler{Service: chatService, Pending: pending}
	realtimeHandler := handlers.RealtimeHandler{Hub: realtimeHub}

	router.Get("/api/health", healthHandler.ServeHTTP)
	router.Get("/api/auth/session", authHandler.Session)
	router.Post("/api/auth/login", authHandler.Login)
	router.Post("/api/auth/logout", authHandler.Logout)
	router.Get("/api/user/app-api-keys", userAppAPIKeysHandler.List)
	router.Post("/api/user/app-api-keys", userAppAPIKeysHandler.Create)
	router.Delete("/api/user/app-api-keys/{keyID}", userAppAPIKeysHandler.Delete)
	router.Get("/api/lab/workspace", labHandler.Workspace)
	router.Get("/api/ws-info", labHandler.PingInfo)
	router.Get("/lab/requests", labHandler.ListRequests)
	router.Get("/lab/requests/{requestID}", labHandler.GetRequest)
	router.Post("/lab/requests/{requestID}/delta", labHandler.RequestDelta)
	router.Post("/lab/requests/{requestID}/complete", labHandler.RequestComplete)
	router.Post("/lab/requests/{requestID}/abort", labHandler.RequestAbort)
	router.Get("/api/ws", realtimeHandler.WebSocket)
	appRouter := chi.NewRouter()
	appRouter.With(
		middleware.RequireAppAPIKey(appAPIKeyService, "requests:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/me", appAPIHandler.Me)
	appRouter.With(
		middleware.RequireAppAPIKey(appAPIKeyService, "requests:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/requests", appAPIHandler.ListRequests)
	appRouter.With(
		middleware.RequireAppAPIKey(appAPIKeyService, "requests:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/requests/{requestID}", appAPIHandler.GetRequest)
	appRouter.With(
		middleware.RequireAppAPIKey(appAPIKeyService, "requests:respond"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Post("/requests/{requestID}/delta", appAPIHandler.RequestDelta)
	appRouter.With(
		middleware.RequireAppAPIKey(appAPIKeyService, "requests:respond"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Post("/requests/{requestID}/complete", appAPIHandler.RequestComplete)
	appRouter.With(
		middleware.RequireAppAPIKey(appAPIKeyService, "requests:respond"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Post("/requests/{requestID}/abort", appAPIHandler.RequestAbort)
	router.Mount("/api/app", appRouter)
	router.Get("/api/conversations/{conversationID}/messages", chatHandler.ListConversationMessages)
	router.Post("/api/conversations/{conversationID}/abort", chatHandler.AbortConversation)
	router.Post("/api/conversations/{conversationID}/respond", chatHandler.RespondConversation)
	router.Post("/api/conversations/{conversationID}/stream/delta", chatHandler.StreamDeltaConversation)
	router.Post("/api/conversations/{conversationID}/stream/complete", chatHandler.RespondConversation)
	router.Post("/api/chat/output/delta", chatHandler.DeltaOutput)
	router.Post("/api/chat/output/complete", chatHandler.CompleteOutput)
	router.Post("/responses", chatHandler.Responses)
	router.Post("/v1/responses", chatHandler.Responses)
	router.Post("/chat/completions", chatHandler.ChatCompletions)
	router.Post("/v1/chat/completions", chatHandler.ChatCompletions)
	router.Post("/messages", chatHandler.AnthropicMessages)
	router.Post("/v1/messages", chatHandler.AnthropicMessages)

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
