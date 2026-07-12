package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zyf2007/ChatAPI/internal/config"
	httphandler "github.com/zyf2007/ChatAPI/internal/http/handler"
)

func registerProtocolRoutes(router chi.Router, cfg config.Config, modelAuth func(http.Handler) http.Handler, modelPrincipalAccess func(http.Handler) http.Handler, chatHandler httphandler.ChatAPIHandler) {
	if cfg.Mode == config.ModeLab {
		router.Get("/v1/models", chatHandler.ListModels)
		router.Post("/v1/responses", chatHandler.Responses)
		router.Post("/v1/chat/completions", chatHandler.ChatCompletions)
		router.Post("/v1/messages", chatHandler.AnthropicMessages)
		return
	}
	router.With(modelAuth, modelPrincipalAccess).Get("/v1/models", chatHandler.ListModels)
	router.With(modelAuth, modelPrincipalAccess).Post("/v1/responses", chatHandler.Responses)
	router.With(modelAuth, modelPrincipalAccess).Post("/v1/chat/completions", chatHandler.ChatCompletions)
	router.With(modelAuth, modelPrincipalAccess).Post("/v1/messages", chatHandler.AnthropicMessages)
}
