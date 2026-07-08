package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zyf/chatapi/internal/config"
	httpmiddleware "github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/service/auth/authn/identity"
	localauth "github.com/zyf/chatapi/internal/service/auth/authn/local"
	authsettings "github.com/zyf/chatapi/internal/service/auth/authn/settings"
	totpsvc "github.com/zyf/chatapi/internal/service/auth/authn/totp"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
	turnquerysvc "github.com/zyf/chatapi/internal/service/chat/turnquery"
	usersvc "github.com/zyf/chatapi/internal/service/user"
	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

type UserHandler struct {
	Config    config.Config
	Identity  *identity.Service
	Users     *usersvc.Service
	Query     *turnquerysvc.Service
	Turn      *turnsvc.Service
	Policy    *policy.Service
	LocalAuth *localauth.Service
	Settings  *authsettings.Service
	TOTP      *totpsvc.Service
	Logger    *zap.Logger
}

func (h UserHandler) Session(w http.ResponseWriter, r *http.Request) {
	settings, err := h.publicSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated":                     false,
			"user":                              nil,
			"totp_enabled":                      false,
			"registration_enabled":              settings.RegistrationEnabled,
			"geetest_enabled":                   settings.GeeTestEnabled,
			"geetest_captcha_id":                settings.GeeTestCaptchaID,
			"current_connection_count":          0,
			"realtime_max_connections_per_user": h.Config.RealtimeMaxConnectionsPerUser,
			"oidc_enabled":                      settings.OIDCEnabled,
			"oidc_provider_name":                settings.OIDCProviderName,
			"local_password_login_enabled":      settings.LocalPasswordLoginEnabled,
			"email_verification_enabled":        settings.EmailVerificationEnabled,
		})
		return
	}
	user, err := h.Identity.GetUser(r.Context(), pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user": map[string]any{
			"id":       user.ID,
			"username": user.Username,
			"role":     h.Policy.EffectiveRole(user),
		},
		"totp_enabled":                      h.TOTP != nil && h.TOTP.IsEnabled(r.Context(), pr.UserID),
		"registration_enabled":              settings.RegistrationEnabled,
		"geetest_enabled":                   settings.GeeTestEnabled,
		"geetest_captcha_id":                settings.GeeTestCaptchaID,
		"current_connection_count":          0,
		"realtime_max_connections_per_user": h.Config.RealtimeMaxConnectionsPerUser,
		"oidc_enabled":                      settings.OIDCEnabled,
		"oidc_provider_name":                settings.OIDCProviderName,
		"local_password_login_enabled":      settings.LocalPasswordLoginEnabled,
		"email_verification_enabled":        settings.EmailVerificationEnabled,
	})
}

func (h UserHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.ownerIDFromContext(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Query.ListConversationsForOwner(r.Context(), ownerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items, "conversations": items})
}

func (h UserHandler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.ownerIDFromContext(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	items, err := h.Query.ListMessagesForOwner(r.Context(), conversationID, ownerID)
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, turnquerysvc.ErrForbidden) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h UserHandler) AbortConversation(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	if _, err := h.Query.ListMessagesForOwner(r.Context(), conversationID, pr.UserID); err != nil {
		status := http.StatusNotFound
		if errors.Is(err, turnquerysvc.ErrForbidden) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, err := h.Turn.ExecuteTurnControl(r.Context(), turnsvc.TurnControlCommand{
		Kind: turnsvc.TurnControlAbort, ConversationID: conversationID, AbortReason: strings.TrimSpace(body.Error),
	})
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h UserHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	conversations, err := h.Query.ListConversationsForOwner(r.Context(), pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	found := false
	for _, item := range conversations {
		if item.ID == conversationID {
			found = true
			if stringValueAny(item.Metadata["realtime_status"], "") == "waiting" {
				http.Error(w, "waiting conversation cannot be deleted", http.StatusConflict)
				return
			}
			break
		}
	}
	if !found {
		http.Error(w, "record not found", http.StatusNotFound)
		return
	}
	result, err := h.Users.DeleteConversation(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (h UserHandler) PruneConversations(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
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
	items, err := h.Query.ListConversationsForOwner(r.Context(), pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	deleteIDs := make([]string, 0)
	skipped := 0
	for idx, item := range items {
		if idx < body.KeepCount {
			continue
		}
		if stringValueAny(item.Metadata["realtime_status"], "") == "waiting" {
			skipped++
			continue
		}
		deleteIDs = append(deleteIDs, item.ID)
	}
	result, err := h.Users.DeleteConversations(r.Context(), deleteIDs)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted_count": result.DeletedConversations,
		"skipped_count": skipped,
		"keep_count":    body.KeepCount,
	})
}

func (h UserHandler) ListAppKeys(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Users.ListAppKeys(r.Context(), pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items, "api_keys": items, "api_key_limit_per_user": 0})
}

func (h UserHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	item, err := h.Users.GetConfig(r.Context(), pr.UserID, "settings")
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	payload := cloneMap(item.Value)
	payload["ok"] = true
	writeJSON(w, http.StatusOK, payload)
}

func (h UserHandler) SetConfig(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, err := h.Users.SetConfig(r.Context(), pr.UserID, "settings", body)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	payload := cloneMap(item.Value)
	payload["ok"] = true
	writeJSON(w, http.StatusOK, payload)
}

func (h UserHandler) CreateAppKey(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
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
	item, rawKey, err := h.Users.CreateAppKey(r.Context(), pr.UserID, body.Name, body.Scopes, body.ResourceLimits, parseOptionalTime(body.ExpiresAt))
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "api_key": map[string]any{
		"id":         item.ID,
		"name":       item.Name,
		"created_at": item.CreatedAt,
		"api_key":    rawKey,
	}})
}

func (h UserHandler) RevokeAppKey(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if err := h.Users.RevokeAppKey(r.Context(), pr.UserID, keyID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h UserHandler) ListModelKeys(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Users.ListModelKeys(r.Context(), pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h UserHandler) CreateModelKey(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
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
	item, rawKey, err := h.Users.CreateModelKey(r.Context(), pr.UserID, body.Name, body.Model)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "model_key": map[string]any{
		"id":         item.ID,
		"name":       item.Name,
		"model":      item.Model,
		"created_at": item.CreatedAt,
		"api_key":    rawKey,
	}})
}

func (h UserHandler) RevokeModelKey(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if err := h.Users.RevokeModelKey(r.Context(), pr.UserID, keyID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h UserHandler) ListIdentities(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Users.ListUserIdentities(r.Context(), pr.UserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h UserHandler) ListAutomationRules(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "session unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Users.ListAutomationRules(r.Context(), pr.UserID)
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": rules})
}

func (h UserHandler) ReplaceAutomationRules(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
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
	inputs := make([]store.UpsertAutomationRuleInput, 0, len(body.Rules))
	for _, item := range body.Rules {
		id := stringValueAny(item["id"], "")
		enabled, _ := item["enabled"].(bool)
		delete(item, "id")
		delete(item, "enabled")
		inputs = append(inputs, store.UpsertAutomationRuleInput{ID: id, UserID: pr.UserID, Enabled: enabled, Payload: item})
	}
	items, err := h.Users.ReplaceAutomationRules(r.Context(), pr.UserID, inputs)
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": rules})
}

func (h UserHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	pr, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
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
	if err := h.LocalAuth.UpdatePasswordForUser(r.Context(), pr.UserID, body.Password); err != nil {
		if errors.Is(err, localauth.ErrNewPasswordRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
	if principal, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context()); ok && strings.TrimSpace(principal.UserID) != "" {
		return strings.TrimSpace(principal.UserID), true
	}
	if principal, ok := httpmiddleware.AppAPIPrincipalFromContext(r.Context()); ok && strings.TrimSpace(principal.UserID) != "" {
		return strings.TrimSpace(principal.UserID), true
	}
	return "", false
}

func (h UserHandler) publicSettings(ctx context.Context) (authsettings.PublicSettings, error) {
	if h.Settings == nil {
		return authsettings.PublicSettings{
			LocalPasswordLoginEnabled: true,
			RegistrationEnabled:       true,
			GeeTestEnabled:            strings.TrimSpace(h.Config.GeetestCaptchaID) != "",
			GeeTestCaptchaID:          strings.TrimSpace(h.Config.GeetestCaptchaID),
			OIDCEnabled:               h.Config.Mode != config.ModeLab && h.Config.OIDCEnabled,
		}, nil
	}
	return h.Settings.Public(ctx)
}
