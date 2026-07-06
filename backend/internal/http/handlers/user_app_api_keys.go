package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

type UserAppAPIKeysHandler struct {
	Config     config.Config
	AppAPIKeys *service.AppAPIKeyService
	Audit      *service.AuditService
}

func (h UserAppAPIKeysHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
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
	name := stringValue(body["name"], "app key")
	scopes := stringSlice(body["scopes"])
	resourceLimits := mapValue(body["resource_limits"])
	expiresAt, err := parseOptionalTime(body["expires_at"])
	if err != nil {
		http.Error(w, "invalid expires_at", http.StatusBadRequest)
		return
	}
	item, rawKey, err := h.AppAPIKeys.CreateKey(r.Context(), userID, name, scopes, resourceLimits, expiresAt)
	if err != nil {
		h.recordAudit(r, "create", "failure", "", map[string]any{
			"name":       name,
			"scopes":     scopes,
			"expires_at": stringValue(body["expires_at"], ""),
			"error":      err.Error(),
		})
		if errors.Is(err, service.ErrInvalidAppAPIKeyExpiry) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var configErr *service.AppAPIKeyConfigError
		if errors.As(err, &configErr) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.recordAudit(r, "create", "success", item.ID, map[string]any{
		"name":       item.Name,
		"scopes":     item.Scopes,
		"expires_at": formatOptionalTime(item.ExpiresAt),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"item":    item,
		"raw_key": rawKey,
	})
}

func (h UserAppAPIKeysHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
	if err := h.AppAPIKeys.RevokeKey(r.Context(), userID, keyID); err != nil {
		h.recordAudit(r, "delete", "failure", keyID, map[string]any{
			"error": err.Error(),
		})
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.recordAudit(r, "delete", "success", keyID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h UserAppAPIKeysHandler) recordAudit(r *http.Request, action string, outcome string, keyID string, metadata map[string]any) {
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "user.app_api_key",
		ResourceType: "app_api_key",
		ResourceID:   keyID,
		Action:       action,
		Outcome:      outcome,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata:     metadata,
	})
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

func parseOptionalTime(value any) (*time.Time, error) {
	raw := strings.TrimSpace(stringValue(value, ""))
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
