package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/http/httpx"
	appkey "github.com/zyf/chatapi/internal/service/auth/authz/appkey"
	"github.com/zyf/chatapi/internal/service/auth/authz/session"
	"github.com/zyf/chatapi/internal/service/usercontrol"
	usercontrolconversations "github.com/zyf/chatapi/internal/service/usercontrol/conversations"
	usercontrolprofile "github.com/zyf/chatapi/internal/service/usercontrol/profile"
	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

type UserHandler struct {
	Config      config.Config
	UserControl *usercontrol.Service
	Logger      *zap.Logger
}

func (h UserHandler) Session(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		view, err := h.UserControl.Profile.BuildAnonymousSessionView(r.Context(), h.Config)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, view)
		return
	}
	view, err := h.UserControl.Profile.BuildAuthenticatedSessionView(r.Context(), h.Config, pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (h UserHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.ownerIDFromContext(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.UserControl.Conversations.ListConversations(r.Context(), ownerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items, "conversations": items})
}

func (h UserHandler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.ownerIDFromContext(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	items, err := h.UserControl.Conversations.ListConversationMessages(r.Context(), ownerID, conversationID)
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, usercontrolconversations.ErrForbidden) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h UserHandler) AbortConversation(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, err := h.UserControl.Conversations.AbortConversation(r.Context(), pr.UserID, conversationID, body.Error)
	if err != nil {
		status := statusForStoreError(err)
		if errors.Is(err, usercontrolconversations.ErrForbidden) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h UserHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	result, err := h.UserControl.Conversations.DeleteConversation(r.Context(), pr.UserID, conversationID)
	if err != nil {
		status := statusForStoreError(err)
		if errors.Is(err, usercontrolconversations.ErrWaitingConversationDelete) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (h UserHandler) PruneConversations(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		KeepCount int `json:"keep_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, skipped, err := h.UserControl.Conversations.PruneConversations(r.Context(), pr.UserID, body.KeepCount)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"deleted_count": result.DeletedConversations,
		"skipped_count": skipped,
		"keep_count":    body.KeepCount,
	})
}

func (h UserHandler) ListAppKeys(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.UserControl.Keys.ListAppKeys(r.Context(), pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items, "api_keys": items, "api_key_limit_per_user": 0})
}

func (h UserHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	item, err := h.UserControl.Config.GetUserConfig(r.Context(), pr.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	payload := cloneMap(item.Value)
	payload["ok"] = true
	httpx.WriteJSON(w, http.StatusOK, payload)
}

func (h UserHandler) SetConfig(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, err := h.UserControl.Config.UpdateUserConfig(r.Context(), pr.UserID, body)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	payload := cloneMap(item.Value)
	payload["ok"] = true
	httpx.WriteJSON(w, http.StatusOK, payload)
}

func (h UserHandler) CreateAppKey(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Name           string         `json:"name"`
		Scopes         []string       `json:"scopes"`
		ResourceLimits map[string]any `json:"resource_limits"`
		ExpiresAt      *string        `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, rawKey, err := h.UserControl.Keys.CreateAppKey(r.Context(), pr.UserID, body.Name, body.Scopes, body.ResourceLimits, parseOptionalTime(body.ExpiresAt))
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "api_key": map[string]any{
		"id":         item.ID,
		"name":       item.Name,
		"created_at": item.CreatedAt,
		"api_key":    rawKey,
	}})
}

func (h UserHandler) RevokeAppKey(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if err := h.UserControl.Keys.RevokeAppKey(r.Context(), pr.UserID, keyID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h UserHandler) ListModelKeys(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.UserControl.Keys.ListModelKeys(r.Context(), pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h UserHandler) CreateModelKey(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, rawKey, err := h.UserControl.Keys.CreateModelKey(r.Context(), pr.UserID, body.Name, body.Model)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "model_key": map[string]any{
		"id":         item.ID,
		"name":       item.Name,
		"model":      item.Model,
		"created_at": item.CreatedAt,
		"api_key":    rawKey,
	}})
}

func (h UserHandler) RevokeModelKey(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if err := h.UserControl.Keys.RevokeModelKey(r.Context(), pr.UserID, keyID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h UserHandler) ListIdentities(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.UserControl.Identity.ListLinkedIdentities(r.Context(), pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h UserHandler) ListAutomationRules(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.UserControl.Config.ListAutomationRules(r.Context(), pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	rules := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rule := cloneMap(item.Payload)
		rule["id"] = item.ID
		rule["enabled"] = item.Enabled
		rules = append(rules, rule)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": rules})
}

func (h UserHandler) ReplaceAutomationRules(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	items, err := h.UserControl.Config.ReplaceAutomationRules(r.Context(), pr.UserID, body.Rules)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	rules := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rule := cloneMap(item.Payload)
		rule["id"] = item.ID
		rule["enabled"] = item.Enabled
		rules = append(rules, rule)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": rules})
}

func (h UserHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	pr, ok := session.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if err := h.UserControl.Profile.ChangePassword(r.Context(), pr.UserID, body.Password); err != nil {
		if errors.Is(err, usercontrolprofile.ErrNewPasswordRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func parseOptionalTime(raw *string) *time.Time {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	if value, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw)); err == nil {
		return &value
	}
	return nil
}

func stringValueAny(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (h UserHandler) ownerIDFromContext(r *http.Request) (string, bool) {
	if principal, ok := session.PrincipalFromContext(r.Context()); ok && strings.TrimSpace(principal.UserID) != "" {
		return strings.TrimSpace(principal.UserID), true
	}
	if principal, ok := appkey.PrincipalFromContext(r.Context()); ok && strings.TrimSpace(principal.UserID) != "" {
		return strings.TrimSpace(principal.UserID), true
	}
	return "", false
}
