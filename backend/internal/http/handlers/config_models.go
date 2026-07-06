package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type ConfigModelsHandler struct {
	Config  config.Config
	Service *service.VirtualModelService
	Audit   *service.AuditService
}

func (h ConfigModelsHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	items, err := h.Service.List(r.Context(), userID)
	if err != nil {
		writeConfigModelsError(w, err)
		return
	}
	writeConfigModelsResponse(w, items)
}

func (h ConfigModelsHandler) Schema(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil || userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"schema": h.Service.Schema(),
	})
}

func (h ConfigModelsHandler) Post(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid model config request", http.StatusBadRequest)
		return
	}
	input := service.VirtualModel{
		ID:      strings.TrimSpace(configModelStringValue(body["id"], "")),
		Name:    strings.TrimSpace(configModelStringValue(body["name"], "")),
		OwnedBy: strings.TrimSpace(configModelStringValue(body["owned_by"], "")),
		Created: int64(configModelNumberValue(body["created"])),
		Enabled: configModelBoolValue(body["enabled"], true),
	}
	items, err := h.Service.Upsert(r.Context(), userID, input)
	if err != nil {
		writeConfigModelsError(w, err)
		return
	}
	h.recordAudit(r, userID, "upsert", input.ID)
	writeConfigModelsResponse(w, items)
}

func (h ConfigModelsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	modelID := strings.TrimSpace(chi.URLParam(r, "modelID"))
	items, err := h.Service.Delete(r.Context(), userID, modelID)
	if err != nil {
		writeConfigModelsError(w, err)
		return
	}
	h.recordAudit(r, userID, "delete", modelID)
	writeConfigModelsResponse(w, items)
}

func writeConfigModelsResponse(w http.ResponseWriter, items []service.VirtualModel) {
	models := make([]string, 0, len(items))
	for _, item := range items {
		models = append(models, item.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"models": models,
		"items":  items,
	})
}

func currentActorUserID(r *http.Request, cfg config.Config) (string, error) {
	if userID := middleware.CurrentUserID(r); userID != "" {
		return userID, nil
	}
	if cfg.Mode == config.ModeLab {
		return "", errors.New("lab request actor is missing")
	}
	return "", errors.New("session required")
}

func (h ConfigModelsHandler) recordAudit(r *http.Request, userID string, action string, modelID string) {
	if h.Audit == nil {
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "user.config",
		ResourceType: "virtual_model",
		ResourceID:   modelID,
		Action:       action,
		Outcome:      "success",
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"user_id": userID,
		},
	})
}

func writeConfigModelsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidVirtualModel):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func configModelStringValue(value any, fallback string) string {
	raw, ok := value.(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	return strings.TrimSpace(raw)
}

func configModelNumberValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case int32:
		return float64(typed)
	default:
		return 0
	}
}

func configModelBoolValue(value any, fallback bool) bool {
	flag, ok := value.(bool)
	if !ok {
		return fallback
	}
	return flag
}
