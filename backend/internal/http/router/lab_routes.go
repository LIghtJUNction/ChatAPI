package router

import (
	"github.com/go-chi/chi/v5"
	httphandler "github.com/zyf/chatapi/internal/http/handler"
)

func registerLabRoutes(router chi.Router, labHandler httphandler.LabHandler) {
	router.Get("/api/lab/workspace", labHandler.Workspace)
	router.Get("/api/ws-info", labHandler.PingInfo)
	router.Get("/lab/requests", labHandler.ListRequests)
	router.Get("/lab/requests/{requestID}", labHandler.GetRequest)
	router.Post("/lab/requests/{requestID}/copy-curl", labHandler.CopyRequestCurl)
	router.Post("/lab/requests/{requestID}/delta", labHandler.RequestDelta)
	router.Post("/lab/requests/{requestID}/complete", labHandler.RequestComplete)
	router.Post("/lab/requests/{requestID}/abort", labHandler.RequestAbort)
}
