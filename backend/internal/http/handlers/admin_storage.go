package handlers

import (
	"net/http"

	"github.com/zyf/chatapi/internal/service"
)

type AdminStorageHandler struct {
	Monitor *service.StorageMonitorService
}

func (h AdminStorageHandler) Summary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.Monitor.Summary(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"summary": summary,
	})
}

func (h AdminStorageHandler) Users(w http.ResponseWriter, r *http.Request) {
	users, err := h.Monitor.Users(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"items": users,
	})
}
