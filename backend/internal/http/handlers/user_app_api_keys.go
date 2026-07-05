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

type UserAppAPIKeysHandler struct {
	Config     config.Config
	AppAPIKeys *service.AppAPIKeyService
}

func (h UserAppAPIKeysHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := h.currentUserID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotImplemented)
		return
	}
	items, err := h.AppAPIKeys.ListKeysForUser(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h UserAppAPIKeysHandler) Create(w http.ResponseWriter, r *http.Request) {
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
	name := stringValue(body["name"], "app key")
	scopes := stringSlice(body["scopes"])
	resourceLimits := mapValue(body["resource_limits"])
	item, rawKey, err := h.AppAPIKeys.CreateKey(r.Context(), userID, name, scopes, resourceLimits)
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

func (h UserAppAPIKeysHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
	if err := h.AppAPIKeys.RevokeKey(r.Context(), userID, keyID); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h UserAppAPIKeysHandler) currentUserID(r *http.Request) (string, error) {
	if actor, ok := service.RequestActorFromContext(r.Context()); ok && strings.TrimSpace(actor.UserID) != "" {
		return strings.TrimSpace(actor.UserID), nil
	}
	if h.Config.Mode == config.ModeLab {
		return "", errors.New("lab request actor is missing")
	}
	return "", errors.New("session-backed app api key management is not implemented yet")
}

func stringSlice(value any) []string {
	switch raw := value.(type) {
	case []string:
		return raw
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			text, _ := item.(string)
			text = strings.TrimSpace(text)
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func mapValue(value any) map[string]any {
	record, _ := value.(map[string]any)
	return record
}
