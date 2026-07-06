package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

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
	httpMetrics := service.NewHTTPMetricsRegistry()
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	router.Use(middleware.RecordHTTPMetrics(httpMetrics))
	router.Use(middleware.InjectLabRequestActor(cfg))
	router.Use(middleware.InjectSessionRequestActor(cfg))
	router.Use(middleware.RequireSessionCSRF(cfg))
	router.Use(middleware.RequireLabAccess(cfg))

	healthHandler := handlers.HealthHandler{Config: cfg, Store: dataStore}
	readinessHandler := handlers.ReadinessHandler{Service: service.NewReadinessService(cfg, dataStore)}
	labHandler := handlers.LabHandler{Config: cfg, Store: dataStore, Service: chatService}
	auditService := service.NewAuditService(dataStore)
	authHandler := handlers.AuthHandler{
		Config:       cfg,
		Audit:        auditService,
		LocalAuth:    service.NewLocalAuthService(dataStore),
		LoginLimiter: service.NewLoginRateLimiter(5, time.Minute),
	}
	uploadsHandler := handlers.UploadsHandler{Service: service.NewUploadService(cfg, dataStore), Audit: auditService}
	appAPIKeyService := service.NewAppAPIKeyService(dataStore)
	modelAPIKeyService := service.NewModelAPIKeyService(dataStore, cfg.MasterKey)
	automationRuleService := service.NewAutomationRuleService(dataStore)
	appAPIHandler := handlers.AppAPIHandler{Service: chatService, ModelAPIKeys: modelAPIKeyService, AutomationRules: automationRuleService}
	userAppAPIKeysHandler := handlers.UserAppAPIKeysHandler{Config: cfg, AppAPIKeys: appAPIKeyService, Audit: auditService}
	userModelAPIKeysHandler := handlers.UserModelAPIKeysHandler{Config: cfg, ModelAPIKeys: modelAPIKeyService, Audit: auditService}
	runtimeMonitor := service.NewRuntimeMonitorService(cfg, realtimeHub, pending)
	adminRuntimeHandler := handlers.AdminRuntimeHandler{Monitor: runtimeMonitor, Audit: auditService}
	metricsHandler := handlers.MetricsHandler{Service: service.NewMetricsService(runtimeMonitor, httpMetrics)}
	storageMonitor := service.NewStorageMonitorService(cfg, dataStore)
	adminStorageHandler := handlers.AdminStorageHandler{Monitor: storageMonitor, Audit: auditService}
	adminRequestsHandler := handlers.AdminRequestsHandler{Service: chatService}
	adminAuditHandler := handlers.AdminAuditHandler{Audit: auditService}
	adminUsersHandler := handlers.AdminUsersHandler{Users: service.NewAdminUserService(dataStore), Audit: auditService}
	chatHandler := handlers.ChatAPIHandler{Service: chatService, Pending: pending}
	realtimeHandler := handlers.RealtimeHandler{Hub: realtimeHub}

	router.Get("/api/health", healthHandler.ServeHTTP)
	router.Get("/api/ready", readinessHandler.ServeHTTP)
	if cfg.MetricsEnabled {
		router.Get("/metrics", metricsHandler.ServeHTTP)
	}
	router.Get("/api/auth/session", authHandler.Session)
	router.Post("/api/auth/login", authHandler.Login)
	router.Post("/api/auth/logout", authHandler.Logout)
	router.Get("/api/user/app-api-keys", userAppAPIKeysHandler.List)
	router.Post("/api/user/app-api-keys", userAppAPIKeysHandler.Create)
	router.Delete("/api/user/app-api-keys/{keyID}", userAppAPIKeysHandler.Delete)
	router.Get("/api/user/model-api-keys", userModelAPIKeysHandler.List)
	router.Post("/api/user/model-api-keys", userModelAPIKeysHandler.Create)
	router.Delete("/api/user/model-api-keys/{keyID}", userModelAPIKeysHandler.Delete)
	router.Get("/api/lab/workspace", labHandler.Workspace)
	router.Get("/api/ws-info", labHandler.PingInfo)
	router.Post("/api/uploads/imgs", uploadsHandler.CreateImage)
	router.Get("/api/uploads/imgs/usage", uploadsHandler.Usage)
	router.Get("/api/uploads/imgs/{filename}", uploadsHandler.Image)
	router.Get("/lab/requests", labHandler.ListRequests)
	router.Get("/lab/requests/{requestID}", labHandler.GetRequest)
	router.Post("/lab/requests/{requestID}/delta", labHandler.RequestDelta)
	router.Post("/lab/requests/{requestID}/complete", labHandler.RequestComplete)
	router.Post("/lab/requests/{requestID}/abort", labHandler.RequestAbort)
	router.Get("/api/ws", realtimeHandler.WebSocket)
	appRouter := chi.NewRouter()
	appAuth := func(scopes ...string) func(http.Handler) http.Handler {
		return middleware.RequireAppAPIKey(appAPIKeyService, cfg.TrustedProxies, scopes...)
	}
	appRouter.With(
		appAuth("requests:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/me", appAPIHandler.Me)
	appRouter.With(
		appAuth("requests:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/requests", appAPIHandler.ListRequests)
	appRouter.With(
		appAuth("requests:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/requests/{requestID}", appAPIHandler.GetRequest)
	appRouter.With(
		appAuth("conversations:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/conversations", appAPIHandler.ListConversations)
	appRouter.With(
		appAuth("conversations:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/conversations/{conversationID}/messages", appAPIHandler.ListConversationMessages)
	appRouter.With(
		appAuth("automation:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/automation-rules", appAPIHandler.ListAutomationRules)
	appRouter.With(
		appAuth("automation:write"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Put("/automation-rules", appAPIHandler.PutAutomationRules)
	appRouter.With(
		appAuth("statistics:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/statistics/summary", appAPIHandler.StatisticsSummary)
	appRouter.With(
		appAuth("model_keys:read"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Get("/model-keys", appAPIHandler.ListModelAPIKeys)
	appRouter.With(
		appAuth("model_keys:write"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Post("/model-keys", appAPIHandler.CreateModelAPIKey)
	appRouter.With(
		appAuth("model_keys:delete"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Delete("/model-keys/{keyID}", appAPIHandler.DeleteModelAPIKey)
	appRouter.With(
		appAuth("requests:respond"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Post("/requests/{requestID}/delta", appAPIHandler.RequestDelta)
	appRouter.With(
		appAuth("requests:respond"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Post("/requests/{requestID}/complete", appAPIHandler.RequestComplete)
	appRouter.With(
		appAuth("requests:respond"),
		middleware.AuditAppAPIRequests(appAPIKeyService),
	).Post("/requests/{requestID}/abort", appAPIHandler.RequestAbort)
	router.Mount("/api/app", appRouter)
	adminRouter := chi.NewRouter()
	adminRouter.Use(middleware.RequireAdminActor())
	adminRouter.Get("/runtime/summary", adminRuntimeHandler.Summary)
	adminRouter.Get("/runtime/memory", adminRuntimeHandler.Memory)
	adminRouter.Get("/runtime/system", adminRuntimeHandler.System)
	adminRouter.Get("/runtime/connections", adminRuntimeHandler.Connections)
	adminRouter.Get("/runtime/queue", adminRuntimeHandler.Queue)
	adminRouter.Get("/runtime/settings", adminRuntimeHandler.Settings)
	adminRouter.Put("/runtime/settings", adminRuntimeHandler.UpdateSettings)
	adminRouter.Post("/runtime/gc", adminRuntimeHandler.GC)
	adminRouter.Get("/storage/summary", adminStorageHandler.Summary)
	adminRouter.Get("/storage/users", adminStorageHandler.Users)
	adminRouter.Put("/storage/users/{ownerID}/quota", adminStorageHandler.SetUserQuota)
	adminRouter.Delete("/storage/users/{ownerID}/quota", adminStorageHandler.DeleteUserQuota)
	adminRouter.Get("/storage/orphans", adminStorageHandler.Orphans)
	adminRouter.Post("/storage/orphans/cleanup", adminStorageHandler.CleanupOrphans)
	adminRouter.Post("/storage/cleanup", adminStorageHandler.Cleanup)
	adminRouter.Post("/storage/vacuum", adminStorageHandler.Vacuum)
	adminRouter.Get("/requests/overview", adminRequestsHandler.Overview)
	adminRouter.Get("/audit/logs", adminAuditHandler.List)
	adminRouter.Get("/users", adminUsersHandler.List)
	adminRouter.Post("/users", adminUsersHandler.Create)
	adminRouter.Put("/users/{userID}/password", adminUsersHandler.ResetPassword)
	adminRouter.Delete("/users/{userID}", adminUsersHandler.Delete)
	router.Mount("/api/admin", adminRouter)
	router.Get("/api/conversations/{conversationID}/messages", chatHandler.ListConversationMessages)
	router.Post("/api/conversations/{conversationID}/abort", chatHandler.AbortConversation)
	router.Post("/api/conversations/{conversationID}/respond", chatHandler.RespondConversation)
	router.Post("/api/conversations/{conversationID}/stream/delta", chatHandler.StreamDeltaConversation)
	router.Post("/api/conversations/{conversationID}/stream/complete", chatHandler.RespondConversation)
	router.Post("/api/chat/output/delta", chatHandler.DeltaOutput)
	router.Post("/api/chat/output/complete", chatHandler.CompleteOutput)
	modelRouter := router.With(middleware.RequireModelAPIKey(cfg, modelAPIKeyService))
	modelRouter.Post("/responses", chatHandler.Responses)
	modelRouter.Post("/v1/responses", chatHandler.Responses)
	modelRouter.Post("/chat/completions", chatHandler.ChatCompletions)
	modelRouter.Post("/v1/chat/completions", chatHandler.ChatCompletions)
	modelRouter.Post("/messages", chatHandler.AnthropicMessages)
	modelRouter.Post("/v1/messages", chatHandler.AnthropicMessages)

	modelRouter.Get("/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "chatapi-lab", "object": "model", "created": 0, "owned_by": "chatapi"},
			},
		})
	})
	modelRouter.Get("/v1/models", func(w http.ResponseWriter, r *http.Request) {
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
