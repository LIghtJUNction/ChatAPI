package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/zyf/chatapi/internal/config"
	httphandler "github.com/zyf/chatapi/internal/http/handler"
	httpmiddleware "github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/ops/observability/httpmetrics"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/ops/readiness"
	"github.com/zyf/chatapi/internal/ops/setup"
	"github.com/zyf/chatapi/internal/platform/media/localstore"
	"github.com/zyf/chatapi/internal/repository/auditrepo"
	"github.com/zyf/chatapi/internal/repository/authrepo"
	"github.com/zyf/chatapi/internal/repository/chatrepo"
	"github.com/zyf/chatapi/internal/repository/configrepo"
	"github.com/zyf/chatapi/internal/repository/platformrepo"
	"github.com/zyf/chatapi/internal/repository/storagerepo"
	"github.com/zyf/chatapi/internal/service/account"
	"github.com/zyf/chatapi/internal/service/admincontrol"
	auditsvc "github.com/zyf/chatapi/internal/service/audit"
	authaccess "github.com/zyf/chatapi/internal/service/auth/access"
	"github.com/zyf/chatapi/internal/service/auth/authn/geetest"
	"github.com/zyf/chatapi/internal/service/auth/authn/identity"
	labauth "github.com/zyf/chatapi/internal/service/auth/authn/lab"
	localauth "github.com/zyf/chatapi/internal/service/auth/authn/local"
	oidcsvc "github.com/zyf/chatapi/internal/service/auth/authn/oidc"
	"github.com/zyf/chatapi/internal/service/auth/authn/ratelimit"
	sessionrestore "github.com/zyf/chatapi/internal/service/auth/authn/sessionrestore"
	authsettings "github.com/zyf/chatapi/internal/service/auth/authn/settings"
	totpsvc "github.com/zyf/chatapi/internal/service/auth/authn/totp"
	"github.com/zyf/chatapi/internal/service/auth/authn/verification"
	appkey "github.com/zyf/chatapi/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf/chatapi/internal/service/auth/authz/modelkey"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	"github.com/zyf/chatapi/internal/service/auth/authz/session"
	preprocesssvc "github.com/zyf/chatapi/internal/service/chat/preprocess"
	"github.com/zyf/chatapi/internal/service/chat/turn"
	"github.com/zyf/chatapi/internal/service/chat/turnquery"
	"github.com/zyf/chatapi/internal/service/usercontrol"
)

type Deps struct {
	Config         config.Config
	ChatRepo       chatrepo.Store
	AuthRepo       authrepo.Store
	ConfigRepo     configrepo.Store
	StorageRepo    storagerepo.Store
	AuditRepo      auditrepo.Store
	PlatformRepo   platformrepo.MaintenanceStore
	Turn           *turn.Service
	Query          *turnquery.Service
	ModelAPIKeys   *modelkey.Service
	AppAPIKeys     *appkey.Service
	Lab            *labauth.Service
	LocalAuth      *localauth.Service
	Verification   *verification.Service
	Policy         *policy.Service
	Access         *authaccess.Service
	AccessSettings *authaccess.SettingsService
	AuthSettings   *authsettings.Service
	GeeTest        *geetest.Service
	TOTP           *totpsvc.Service
	OIDC           *oidcsvc.Service
	LoginLimiter   *ratelimit.Service
	AdminControl   *admincontrol.Service
	Audit          *auditsvc.Service
	Accounts       *account.Service
	Identity       *identity.Service
	UserControl    *usercontrol.Service
	UserSessions   *session.Service
	LoggerFactory  *logging.Factory
}

func New(deps Deps) http.Handler {
	router := chi.NewRouter()

	httpLogger := deps.logger(logging.LayerHTTP)
	authLogger := deps.logger(logging.LayerAuth)
	if deps.Lab == nil {
		deps.Lab = labauth.NewService(deps.Config)
	}
	if deps.AccessSettings == nil && deps.AuthRepo != nil {
		deps.AccessSettings = authaccess.NewSettingsService(deps.AuthRepo, authaccess.Settings{
			GlobalRateLimitRequests: deps.Config.AccessRateLimitRequests,
			GlobalRateLimitWindow:   deps.Config.AccessRateLimitWindow,
		})
	}
	accessPolicy := deps.Access
	if accessPolicy == nil {
		accessPolicy = authaccess.NewService(deps.Config, deps.Lab, deps.AccessSettings)
	}
	metricsRegistry := httpmetrics.NewRegistry()

	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   deps.Config.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-ChatAPI-App-Key", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	router.Use(httpmiddleware.RecordHTTPMetrics(metricsRegistry))
	router.Use(requestLoggingMiddleware(httpLogger))
	router.Use(httpmiddleware.RequireLabAccess(accessPolicy, authLogger))
	router.Use(httpmiddleware.RequireAccessRateLimit(accessPolicy))
	router.Use(httpmiddleware.LoadLabActor(accessPolicy, authLogger))
	router.Use(httpmiddleware.LoadUserSession(sessionrestore.NewService(deps.UserSessions), authLogger))
	router.Use(httpmiddleware.RequireSessionCSRF(accessPolicy, deps.Policy, authLogger))

	if deps.UserControl == nil {
		deps.UserControl = usercontrol.New(usercontrol.Deps{
			Identity:     deps.Identity,
			LocalAuth:    deps.LocalAuth,
			Settings:     deps.AuthSettings,
			TOTP:         deps.TOTP,
			Policy:       deps.Policy,
			Query:        deps.Query,
			Turn:         deps.Turn,
			Configs:      deps.ConfigRepo,
			Storage:      deps.StorageRepo,
			Chat:         deps.ChatRepo,
			AppKeysStore: deps.AuthRepo,
			AppKeys:      deps.AppAPIKeys,
			ModelKeys:    deps.ModelAPIKeys,
			Accounts:     deps.Accounts,
			Logger:       deps.logger(logging.LayerUserControl),
		})
	}
	if deps.AdminControl == nil {
		deps.AdminControl = admincontrol.New(admincontrol.Deps{
			Accounts:       deps.Accounts,
			Query:          deps.Query,
			Turn:           deps.Turn,
			ChatStore:      deps.ChatRepo,
			StorageStore:   deps.StorageRepo,
			KeyStore:       deps.AuthRepo,
			AuthSettings:   deps.AuthSettings,
			AccessSettings: deps.AccessSettings,
		})
	}

	chatHandler := httphandler.ChatAPIHandler{
		Turn:  deps.Turn,
		Query: deps.Query,
		Preprocess: preprocesssvc.New(deps.Config, localstore.Store{
			RootDir: deps.Config.MediaDerivedDir,
		}),
		Logger: deps.logger(logging.LayerHTTP),
	}
	appHandler := httphandler.AppAPIHandler{
		Turn:   deps.Turn,
		Query:  deps.Query,
		Logger: deps.logger(logging.LayerTurnQuery),
	}
	authHandler := httphandler.AuthHandler{
		Config:       deps.Config,
		LocalAuth:    deps.LocalAuth,
		Verification: deps.Verification,
		Policy:       deps.Policy,
		Settings:     deps.AuthSettings,
		GeeTest:      deps.GeeTest,
		TOTP:         deps.TOTP,
		OIDC:         deps.OIDC,
		Audit:        deps.Audit,
		LoginLimiter: deps.LoginLimiter,
		Sessions:     deps.UserSessions,
		Logger:       deps.logger(logging.LayerAuth),
	}
	userHandler := httphandler.UserHandler{
		Config:      deps.Config,
		UserControl: deps.UserControl,
		Logger:      deps.logger(logging.LayerAuth),
	}
	adminHandler := httphandler.AdminHandler{
		Control: deps.AdminControl,
		Audit:   deps.Audit,
		Logger:  deps.logger(logging.LayerAudit),
	}
	labHandler := httphandler.LabHandler{
		Config: deps.Config,
		Query:  deps.Query,
		Turn:   deps.Turn,
		Logger: deps.logger(logging.LayerHTTP),
	}
	healthHandler := httphandler.HealthHandler{Config: deps.Config, Store: deps.PlatformRepo}
	readinessHandler := httphandler.ReadinessHandler{Service: readiness.NewService(deps.Config, deps.PlatformRepo)}
	setupHandler := httphandler.SetupHandler{Service: setup.NewService(deps.AuthRepo, deps.Config)}
	metricsHandler := httphandler.MetricsHandler{Registry: metricsRegistry}

	modelAuth := httpmiddleware.RequireModelAPIKey(deps.ModelAPIKeys, authLogger)
	modelPrincipalAccess := httpmiddleware.RequirePrincipalAccess(accessPolicy, authLogger)
	appAuth := func(scopes ...string) func(http.Handler) http.Handler {
		return chainMiddleware(
			httpmiddleware.RequireAppAPIKey(deps.AppAPIKeys, deps.Policy, deps.Config.TrustedProxies, authLogger, scopes...),
			httpmiddleware.RequirePrincipalAccess(accessPolicy, authLogger),
		)
	}
	userAuth := httpmiddleware.RequireUserSession(deps.Policy, authLogger)
	userPrincipalAccess := httpmiddleware.RequirePrincipalAccess(accessPolicy, authLogger)
	userOrAppAuth := func(scopes ...string) func(http.Handler) http.Handler {
		return httpmiddleware.RequireUserSessionOrAppAPI(deps.Policy, appAuth(scopes...), userAuth)
	}
	adminAuth := func(next http.Handler) http.Handler {
		return httpmiddleware.RequireAdmin(deps.Policy, authLogger)(httpmiddleware.RequireUserSession(deps.Policy, authLogger)(next))
	}

	router.Get("/api/health", healthHandler.ServeHTTP)
	router.Get("/api/ready", readinessHandler.ServeHTTP)
	router.Get("/api/setup/status", setupHandler.Status)
	router.Get("/setup", setupHandler.HTML)
	router.Post("/setup", setupHandler.Create)
	if deps.Config.MetricsEnabled {
		router.Get("/metrics", metricsHandler.ServeHTTP)
	}

	registerProtocolRoutes(router, deps.Config, modelAuth, modelPrincipalAccess, chatHandler)
	if deps.Config.Mode == config.ModeLab {
		registerLabRoutes(router, labHandler)
		mountSPA(router, deps.Config)
		return router
	}

	router.Post("/api/auth/register", authHandler.Register)
	router.Get("/api/auth/register/config", authHandler.RegisterConfig)
	router.Post("/api/auth/register/send-code", authHandler.RegisterSendCode)
	router.Post("/api/auth/login", authHandler.Login)
	router.Post("/api/auth/logout", authHandler.Logout)
	router.Get("/api/auth/session", userHandler.Session)
	router.Get("/api/auth/oidc/config", authHandler.OIDCConfig)
	router.Get("/api/auth/oidc/login", authHandler.OIDCLogin)
	router.With(userAuth, userPrincipalAccess).Get("/api/auth/oidc/link", authHandler.OIDCLink)
	router.Get("/api/auth/oidc/callback", authHandler.OIDCCallback)
	router.Get("/api/auth/password/config", authHandler.PasswordConfig)
	router.Post("/api/auth/password/send-code", authHandler.PasswordSendCode)
	router.Post("/api/auth/verification/send", authHandler.SendVerification)
	router.Post("/api/auth/verification/verify", authHandler.VerifyCode)
	router.Post("/api/auth/password/forgot", authHandler.ForgotPassword)
	router.Post("/api/auth/password/reset", authHandler.ResetPassword)
	router.With(userAuth, userPrincipalAccess).Get("/api/auth/totp/setup", authHandler.TOTPSetup)
	router.With(userAuth, userPrincipalAccess).Post("/api/auth/totp/confirm", authHandler.TOTPConfirm)
	router.With(userAuth, userPrincipalAccess).Post("/api/auth/totp/reset", authHandler.TOTPReset)

	router.With(userOrAppAuth("conversations:read")).Get("/api/conversations", userHandler.ListConversations)
	router.With(userOrAppAuth("conversations:read")).Get("/api/conversations/{conversationID}/messages", userHandler.ListConversationMessages)
	router.With(userAuth, userPrincipalAccess).Post("/api/conversations/{conversationID}/abort", userHandler.AbortConversation)
	router.With(userAuth, userPrincipalAccess).Delete("/api/conversations/{conversationID}", userHandler.DeleteConversation)
	router.With(userAuth, userPrincipalAccess).Post("/api/conversations/prune", userHandler.PruneConversations)

	router.With(userAuth, userPrincipalAccess).Get("/api/user/app-keys", userHandler.ListAppKeys)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/app-keys", userHandler.CreateAppKey)
	router.With(userAuth, userPrincipalAccess).Delete("/api/user/app-keys/{keyID}", userHandler.RevokeAppKey)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/model-keys", userHandler.ListModelKeys)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/model-keys", userHandler.CreateModelKey)
	router.With(userAuth, userPrincipalAccess).Delete("/api/user/model-keys/{keyID}", userHandler.RevokeModelKey)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/identities", userHandler.ListIdentities)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/config", userHandler.GetConfig)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/config", userHandler.SetConfig)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/password", userHandler.ChangePassword)
	router.With(userAuth, userPrincipalAccess).Get("/api/config/automation-rules", userHandler.ListAutomationRules)
	router.With(userAuth, userPrincipalAccess).Post("/api/config/automation-rules", userHandler.ReplaceAutomationRules)

	router.With(userAuth, userPrincipalAccess).Get("/api/user/api-keys", userHandler.ListAppKeys)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/api-keys", userHandler.CreateAppKey)
	router.With(userAuth, userPrincipalAccess).Delete("/api/user/api-keys/{keyID}", userHandler.RevokeAppKey)

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
	router.With(adminAuth).Get("/api/admin/auth/settings", adminHandler.GetAuthSettings)
	router.With(adminAuth).Post("/api/admin/auth/settings", adminHandler.SetAuthSettings)
	router.With(adminAuth).Get("/api/admin/access/settings", adminHandler.GetAccessSettings)
	router.With(adminAuth).Post("/api/admin/access/settings", adminHandler.SetAccessSettings)

	router.With(appAuth("requests:read")).Get("/api/requests", appHandler.ListRequests)
	router.With(appAuth("requests:read")).Get("/api/requests/{requestID}", appHandler.GetRequest)
	router.With(userOrAppAuth("requests:respond")).Post("/api/chat/output/delta", chatHandler.DeltaOutput)
	router.With(userOrAppAuth("requests:respond")).Post("/api/chat/output/complete", chatHandler.CompleteOutput)
	router.With(userOrAppAuth("requests:respond")).Post("/api/chat/output/abort", chatHandler.AbortOutput)

	mountSPA(router, deps.Config)
	return router
}
