package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	appkey "github.com/zyf/chatapi/internal/apikey/app"
	modelkey "github.com/zyf/chatapi/internal/apikey/model"
	"github.com/zyf/chatapi/internal/config"
	httpmiddleware "github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/observability/logging"
	"github.com/zyf/chatapi/internal/service/chat/turn"
	"github.com/zyf/chatapi/internal/service/chat/turnquery"
)

type RouterDeps struct {
	Config        config.Config
	Turn          *turn.Service
	Query         *turnquery.Service
	ModelAPIKeys  *modelkey.Service
	AppAPIKeys    *appkey.Service
	LoggerFactory *logging.Factory
}

func NewRouter(deps RouterDeps) http.Handler {
	router := chi.NewRouter()

	httpLogger := deps.logger(logging.LayerHTTP)
	authLogger := deps.logger(logging.LayerAuth)

	router.Use(requestLoggingMiddleware(httpLogger))

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

	modelAuth := httpmiddleware.RequireModelAPIKey(deps.ModelAPIKeys, authLogger)
	appAuth := func(scopes ...string) func(http.Handler) http.Handler {
		return httpmiddleware.RequireAppAPIKey(deps.AppAPIKeys, deps.Config.TrustedProxies, authLogger, scopes...)
	}

	router.With(modelAuth).Post("/v1/responses", chatHandler.Responses)
	router.With(modelAuth).Post("/v1/chat/completions", chatHandler.ChatCompletions)
	router.With(modelAuth).Post("/v1/messages", chatHandler.AnthropicMessages)

	router.With(appAuth("requests:read")).Get("/api/requests", appHandler.ListRequests)
	router.With(appAuth("requests:read")).Get("/api/requests/{requestID}", appHandler.GetRequest)
	router.With(appAuth("conversations:read")).Get("/api/conversations", appHandler.ListConversations)
	router.With(appAuth("conversations:read")).Get("/api/conversations/{conversationID}/messages", appHandler.ListConversationMessages)

	router.With(appAuth("requests:respond")).Post("/api/chat/output/delta", chatHandler.DeltaOutput)
	router.With(appAuth("requests:respond")).Post("/api/chat/output/complete", chatHandler.CompleteOutput)
	router.With(appAuth("requests:respond")).Post("/api/chat/output/abort", chatHandler.AbortOutput)

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
