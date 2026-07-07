package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zyf/chatapi/internal/config"
	httpmiddleware "github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	auditsvc "github.com/zyf/chatapi/internal/service/audit"
	authadmin "github.com/zyf/chatapi/internal/service/auth/admin"
	appkey "github.com/zyf/chatapi/internal/service/auth/appkey"
	"github.com/zyf/chatapi/internal/service/auth/identity"
	localauth "github.com/zyf/chatapi/internal/service/auth/local"
	modelkey "github.com/zyf/chatapi/internal/service/auth/modelkey"
	"github.com/zyf/chatapi/internal/service/auth/policy"
	"github.com/zyf/chatapi/internal/service/auth/session"
	"github.com/zyf/chatapi/internal/service/auth/verification"
	chatadmin "github.com/zyf/chatapi/internal/service/chat/admin"
	"github.com/zyf/chatapi/internal/service/chat/turn"
	"github.com/zyf/chatapi/internal/service/chat/turnquery"
	usersvc "github.com/zyf/chatapi/internal/service/user"
)

type RouterDeps struct {
	Config        config.Config
	Turn          *turn.Service
	Query         *turnquery.Service
	ModelAPIKeys  *modelkey.Service
	AppAPIKeys    *appkey.Service
	LocalAuth     *localauth.Service
	Verification  *verification.Service
	Policy        *policy.Service
	AdminUsers    *authadmin.Service
	AdminChat     *chatadmin.Service
	Audit         *auditsvc.Service
	Identity      *identity.Service
	Users         *usersvc.Service
	UserSessions  *session.Service
	LoggerFactory *logging.Factory
}

func NewRouter(deps RouterDeps) http.Handler {
	router := chi.NewRouter()

	httpLogger := deps.logger(logging.LayerHTTP)
	authLogger := deps.logger(logging.LayerAuth)

	router.Use(requestLoggingMiddleware(httpLogger))
	router.Use(httpmiddleware.LoadUserSession(deps.UserSessions, authLogger))

	chatHandler := ChatAPIHandler{
		Turn:   deps.Turn,
		Query:  deps.Query,
		Logger: deps.logger(logging.LayerHTTP),
	}
	appHandler := AppAPIHandler{
		Turn:   deps.Turn,
		Query:  deps.Query,
		Logger: deps.logger(logging.LayerTurnQuery),
	}
	authHandler := AuthHandler{
		LocalAuth:    deps.LocalAuth,
		Verification: deps.Verification,
		Sessions:     deps.UserSessions,
		Logger:       deps.logger(logging.LayerAuth),
	}
	userHandler := UserHandler{
		Config:    deps.Config,
		Identity:  deps.Identity,
		Users:     deps.Users,
		Query:     deps.Query,
		Turn:      deps.Turn,
		Policy:    deps.Policy,
		LocalAuth: deps.LocalAuth,
		Logger:    deps.logger(logging.LayerAuth),
	}
	adminHandler := AdminHandler{
		Users:  deps.AdminUsers,
		Chat:   deps.AdminChat,
		Audit:  deps.Audit,
		Logger: deps.logger(logging.LayerAudit),
	}

	modelAuth := httpmiddleware.RequireModelAPIKey(deps.ModelAPIKeys, authLogger)
	appAuth := func(scopes ...string) func(http.Handler) http.Handler {
		return httpmiddleware.RequireAppAPIKey(deps.AppAPIKeys, deps.Config.TrustedProxies, authLogger, scopes...)
	}
	userAuth := httpmiddleware.RequireUserSession(authLogger)
	userOrAppAuth := func(scopes ...string) func(http.Handler) http.Handler {
		return httpmiddleware.RequireUserSessionOrAppAPI(appAuth(scopes...), userAuth)
	}
	adminAuth := func(next http.Handler) http.Handler {
		return httpmiddleware.RequireAdmin(deps.Policy, authLogger)(httpmiddleware.RequireUserSession(authLogger)(next))
	}

	router.With(modelAuth).Post("/v1/responses", chatHandler.Responses)
	router.With(modelAuth).Post("/v1/chat/completions", chatHandler.ChatCompletions)
	router.With(modelAuth).Post("/v1/messages", chatHandler.AnthropicMessages)

	router.Post("/api/auth/register", authHandler.Register)
	router.Post("/api/auth/login", authHandler.Login)
	router.Post("/api/auth/logout", authHandler.Logout)
	router.Get("/api/auth/session", userHandler.Session)
	router.Post("/api/auth/verification/send", authHandler.SendVerification)
	router.Post("/api/auth/verification/verify", authHandler.VerifyCode)
	router.Post("/api/auth/password/forgot", authHandler.ForgotPassword)
	router.Post("/api/auth/password/reset", authHandler.ResetPassword)

	router.With(userOrAppAuth("conversations:read")).Get("/api/conversations", userHandler.ListConversations)
	router.With(userOrAppAuth("conversations:read")).Get("/api/conversations/{conversationID}/messages", userHandler.ListConversationMessages)
	router.With(userAuth).Post("/api/conversations/{conversationID}/abort", userHandler.AbortConversation)
	router.With(userAuth).Delete("/api/conversations/{conversationID}", userHandler.DeleteConversation)
	router.With(userAuth).Post("/api/conversations/prune", userHandler.PruneConversations)

	router.With(userAuth).Get("/api/user/app-keys", userHandler.ListAppKeys)
	router.With(userAuth).Post("/api/user/app-keys", userHandler.CreateAppKey)
	router.With(userAuth).Delete("/api/user/app-keys/{keyID}", userHandler.RevokeAppKey)
	router.With(userAuth).Get("/api/user/model-keys", userHandler.ListModelKeys)
	router.With(userAuth).Post("/api/user/model-keys", userHandler.CreateModelKey)
	router.With(userAuth).Delete("/api/user/model-keys/{keyID}", userHandler.RevokeModelKey)
	router.With(userAuth).Get("/api/user/identities", userHandler.ListIdentities)
	router.With(userAuth).Get("/api/user/config", userHandler.GetConfig)
	router.With(userAuth).Post("/api/user/config", userHandler.SetConfig)
	router.With(userAuth).Post("/api/user/password", userHandler.ChangePassword)
	router.With(userAuth).Get("/api/config/automation-rules", userHandler.ListAutomationRules)
	router.With(userAuth).Post("/api/config/automation-rules", userHandler.ReplaceAutomationRules)

	router.With(userAuth).Get("/api/user/api-keys", userHandler.ListAppKeys)
	router.With(userAuth).Post("/api/user/api-keys", userHandler.CreateAppKey)
	router.With(userAuth).Delete("/api/user/api-keys/{keyID}", userHandler.RevokeAppKey)

	router.With(adminAuth).Get("/api/admin/users", adminHandler.ListUsers)
	router.With(adminAuth).Post("/api/admin/users", adminHandler.CreateUser)
	router.With(adminAuth).Get("/api/admin/users/{userID}", adminHandler.GetUser)
	router.With(adminAuth).Post("/api/admin/users/{userID}/disable", adminHandler.DisableUser)
	router.With(adminAuth).Post("/api/admin/users/{userID}/enable", adminHandler.EnableUser)
	router.With(adminAuth).Put("/api/admin/users/{userID}/password", adminHandler.ResetUserPassword)
	router.With(adminAuth).Get("/api/admin/users/{userID}/identities", adminHandler.ListUserIdentities)
	router.With(adminAuth).Delete("/api/admin/users/{userID}/identities/{identityID}", adminHandler.DeleteUserIdentity)
	router.With(adminAuth).Get("/api/admin/users/{userID}/app-keys", adminHandler.ListUserAppKeys)
	router.With(adminAuth).Delete("/api/admin/users/{userID}/app-keys/{keyID}", adminHandler.RevokeUserAppKey)
	router.With(adminAuth).Get("/api/admin/users/{userID}/model-keys", adminHandler.ListUserModelKeys)
	router.With(adminAuth).Delete("/api/admin/users/{userID}/model-keys/{keyID}", adminHandler.RevokeUserModelKey)
	router.With(adminAuth).Get("/api/admin/users/{userID}/delete-preview", adminHandler.DeletePreview)
	router.With(adminAuth).Delete("/api/admin/users/{userID}", adminHandler.DeleteUser)
	router.With(adminAuth).Post("/api/admin/users/{userID}/transfer-ownership", adminHandler.TransferOwnership)
	router.With(adminAuth).Get("/api/admin/users/{userID}/ownership-items", adminHandler.OwnershipItems)
	router.With(adminAuth).Post("/api/admin/users/{userID}/transfer-ownership-selection", adminHandler.TransferOwnershipSelection)

	router.With(adminAuth).Get("/api/admin/requests", adminHandler.ListRequests)
	router.With(adminAuth).Get("/api/admin/requests/{requestID}", adminHandler.GetRequest)
	router.With(adminAuth).Get("/api/admin/conversations", adminHandler.ListConversations)
	router.With(adminAuth).Get("/api/admin/conversations/{conversationID}/messages", adminHandler.ListConversationMessages)
	router.With(adminAuth).Post("/api/admin/conversations/{conversationID}/abort", adminHandler.AbortConversation)
	router.With(adminAuth).Post("/api/admin/conversations/{conversationID}/complete", adminHandler.CompleteConversation)
	router.With(adminAuth).Delete("/api/admin/conversations/{conversationID}", adminHandler.DeleteConversation)
	router.With(adminAuth).Get("/api/admin/audit/logs", adminHandler.ListAuditLogs)

	router.With(appAuth("requests:read")).Get("/api/requests", appHandler.ListRequests)
	router.With(appAuth("requests:read")).Get("/api/requests/{requestID}", appHandler.GetRequest)
	router.With(userOrAppAuth("requests:respond")).Post("/api/chat/output/delta", chatHandler.DeltaOutput)
	router.With(userOrAppAuth("requests:respond")).Post("/api/chat/output/complete", chatHandler.CompleteOutput)
	router.With(userOrAppAuth("requests:respond")).Post("/api/chat/output/abort", chatHandler.AbortOutput)

	return router
}

func (d RouterDeps) logger(layer string) *zap.Logger {
	if d.LoggerFactory == nil {
		return zap.NewNop()
	}
	return d.LoggerFactory.Layer(layer)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func requestLoggingMiddleware(base *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			logger := base.With(
				zap.String("http.method", r.Method),
				zap.String("http.path", r.URL.Path),
				zap.String("http.remote_addr", r.RemoteAddr),
			)
			ctx := logging.WithLogger(r.Context(), logger)
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))
			logger = logger.With(
				zap.Int("http.status_code", rec.status),
				zap.Duration("http.duration", time.Since(start)),
			)
			logger.Info("http request completed")
		})
	}
}
