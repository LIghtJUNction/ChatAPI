package handlers

import (
	"net/http"

	"github.com/zyf/chatapi/internal/service"
)

type AdminRequestsHandler struct {
	Service *service.ChatAPIService
}

func (h AdminRequestsHandler) Schema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"schema": service.BuildAdminRequestsSchema(),
	})
}

func (h AdminRequestsHandler) Overview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.Service.RequestsOverview(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"overview": overview,
	})
}
