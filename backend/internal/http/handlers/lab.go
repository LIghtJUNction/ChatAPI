package handlers

import (
	"net/http"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

type LabHandler struct {
	Config config.Config
	Store  store.Store
}

func (h LabHandler) Workspace(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListConversations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"mode":          h.Config.Mode,
		"conversations": items,
	})
}

func (h LabHandler) PingInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "use websocket on /api/ws",
	})
}
