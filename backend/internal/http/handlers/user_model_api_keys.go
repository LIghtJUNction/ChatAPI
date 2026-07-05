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
}

func (h UserModelAPIKeysHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := h.currentUserID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotImplemented)
		return
	}
	items, err := h.ModelAPIKeys.ListKeysForUser(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h UserModelAPIKeysHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := h.currentUserID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotImplemented)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, rawKey, err := h.ModelAPIKeys.CreateKey(r.Context(), userID, stringValue(body["name"], "model key"), stringValue(body["model"], ""))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"item":    item,
		"raw_key": rawKey,
	})
}

func (h UserModelAPIKeysHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, err := h.currentUserID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotImplemented)
		return
	}
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if keyID == "" {
		http.Error(w, "key_id is required", http.StatusBadRequest)
		return
	}
	if err := h.ModelAPIKeys.RevokeKey(r.Context(), userID, keyID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h UserModelAPIKeysHandler) currentUserID(r *http.Request) (string, error) {
	if actor, ok := service.RequestActorFromContext(r.Context()); ok && strings.TrimSpace(actor.UserID) != "" {
		return strings.TrimSpace(actor.UserID), nil
	}
	if h.Config.Mode == config.ModeLab {
		return "", errors.New("lab request actor is missing")
	}
	return "", errors.New("session-backed model api key management is not implemented yet")
}
