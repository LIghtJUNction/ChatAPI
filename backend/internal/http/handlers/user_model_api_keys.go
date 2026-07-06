package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

type UserModelAPIKeysHandler struct {
	Config       config.Config
	ModelAPIKeys *service.ModelAPIKeyService
	Audit        *service.AuditService
}

func (h UserModelAPIKeysHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	items, err := h.ModelAPIKeys.ListKeysForUser(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h UserModelAPIKeysHandler) Schema(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil || userID == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"schema": h.ModelAPIKeys.Schema(),
	})
}

func (h UserModelAPIKeysHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, rawKey, err := h.ModelAPIKeys.CreateKey(r.Context(), userID, stringValue(body["name"], "model key"), stringValue(body["model"], ""))
	if err != nil {
		h.recordAudit(r, "create", "failure", "", map[string]any{
			"name":  stringValue(body["name"], "model key"),
			"model": stringValue(body["model"], ""),
			"error": err.Error(),
		})
		if errors.Is(err, service.ErrModelRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.recordAudit(r, "create", "success", item.ID, map[string]any{
		"name":  item.Name,
		"model": item.Model,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"item":    item,
		"raw_key": rawKey,
	})
}

func (h UserModelAPIKeysHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if keyID == "" {
		http.Error(w, "key_id is required", http.StatusBadRequest)
		return
	}
	if err := h.ModelAPIKeys.RevokeKey(r.Context(), userID, keyID); err != nil {
		h.recordAudit(r, "delete", "failure", keyID, map[string]any{
			"error": err.Error(),
		})
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.recordAudit(r, "delete", "success", keyID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h UserModelAPIKeysHandler) recordAudit(r *http.Request, action string, outcome string, keyID string, metadata map[string]any) {
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "user.model_api_key",
		ResourceType: "model_api_key",
		ResourceID:   keyID,
		Action:       action,
		Outcome:      outcome,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata:     metadata,
	})
}
