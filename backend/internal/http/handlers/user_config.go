package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

type UserConfigHandler struct {
	Config  config.Config
	Service *service.UserConfigService
	Audit   *service.AuditService
}

func (h UserConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := h.currentUserID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	items, configMap, err := h.Service.List(r.Context(), userID)
	if err != nil {
		writeUserConfigError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"items":  items,
		"config": configMap,
	})
}

func (h UserConfigHandler) Schema(w http.ResponseWriter, r *http.Request) {
	userID, err := h.currentUserID(r)
	if err != nil || userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"schema": h.Service.Schema(),
	})
}

func (h UserConfigHandler) Set(w http.ResponseWriter, r *http.Request) {
	userID, err := h.currentUserID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid user config request", http.StatusBadRequest)
		return
	}
	values := body
	if nested, ok := body["config"].(map[string]any); ok {
		values = nested
	}
	items, configMap, err := h.Service.SetMany(r.Context(), userID, values)
	if err != nil {
		writeUserConfigError(w, err)
		return
	}
	h.recordAudit(r, userID, values)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"items":  items,
		"config": configMap,
	})
}

func (h UserConfigHandler) currentUserID(r *http.Request) (string, error) {
	if actor, ok := service.RequestActorFromContext(r.Context()); ok && strings.TrimSpace(actor.UserID) != "" {
		return strings.TrimSpace(actor.UserID), nil
	}
	if h.Config.Mode == config.ModeLab {
		return "", errors.New("lab request actor is missing")
	}
	return "", errors.New("session required")
}

func (h UserConfigHandler) recordAudit(r *http.Request, userID string, values map[string]any) {
	if h.Audit == nil {
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "user.config",
		ResourceType: "user_config",
		ResourceID:   userID,
		Action:       "update",
		Outcome:      "success",
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"keys": keysOfMap(values),
		},
	})
}

func writeUserConfigError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrInvalidUserConfig) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func keysOfMap(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}
