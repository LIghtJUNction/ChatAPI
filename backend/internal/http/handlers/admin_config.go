package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zyf/chatapi/internal/service"
)

type AdminConfigHandler struct {
	Service *service.SystemConfigService
	Audit   *service.AuditService
}

func (h AdminConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	items, configMap, err := h.Service.List(r.Context())
	if err != nil {
		writeSystemConfigError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"items":  items,
		"config": configMap,
	})
}

func (h AdminConfigHandler) Set(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid system config request", http.StatusBadRequest)
		return
	}
	values := body
	if nested, ok := body["config"].(map[string]any); ok {
		values = nested
	}
	items, configMap, err := h.Service.SetMany(r.Context(), values)
	if err != nil {
		writeSystemConfigError(w, err)
		return
	}
	h.recordAudit(r, values)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"items":  items,
		"config": configMap,
	})
}

func (h AdminConfigHandler) recordAudit(r *http.Request, values map[string]any) {
	if h.Audit == nil {
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "admin.config",
		ResourceType: "system_config",
		Action:       "update",
		Outcome:      "success",
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"keys": keysOfMap(values),
		},
	})
}

func writeSystemConfigError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrInvalidSystemConfig) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
