package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/zyf/chatapi/internal/service"
)

type AdminAuditHandler struct {
	Audit *service.AuditService
}

func (h AdminAuditHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = value
	}
	items, err := h.Audit.List(r.Context(), service.ListAuditLogsInput{
		Limit:       limit,
		EventType:   r.URL.Query().Get("event_type"),
		ActorUserID: r.URL.Query().Get("actor_user_id"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"count": len(items),
		"items": items,
	})
}
