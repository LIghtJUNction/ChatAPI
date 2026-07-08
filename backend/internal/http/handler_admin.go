package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	auditsvc "github.com/zyf/chatapi/internal/service/audit"
	authaccess "github.com/zyf/chatapi/internal/service/auth/access"
	authadmin "github.com/zyf/chatapi/internal/service/auth/admin"
	authsettings "github.com/zyf/chatapi/internal/service/auth/authn/settings"
	chatadmin "github.com/zyf/chatapi/internal/service/chat/admin"
	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

type AdminHandler struct {
	Users          *authadmin.Service
	Chat           *chatadmin.Service
	Audit          *auditsvc.Service
	AuthSettings   *authsettings.Service
	AccessSettings *authaccess.SettingsService
	Logger         *zap.Logger
}

func (h AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	items, err := h.Users.ListUsers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	item, err := h.Users.GetUser(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": item})
}

func (h AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username   string `json:"username"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		Role       string `json:"role"`
		IsActive   *bool  `json:"is_active"`
		LocalAdmin bool   `json:"local_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}
	item, err := h.Users.CreateUser(r.Context(), authadmin.CreateUserInput{
		Username:   body.Username,
		Email:      body.Email,
		Password:   body.Password,
		Role:       body.Role,
		IsActive:   isActive,
		LocalAdmin: body.LocalAdmin,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.record(r, "admin.user", "user", item.ID, "create", "success", map[string]any{"email": item.Email})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "user": item})
}

func (h AdminHandler) DisableUser(w http.ResponseWriter, r *http.Request) {
	h.setUserState(w, r, false)
}
func (h AdminHandler) EnableUser(w http.ResponseWriter, r *http.Request) { h.setUserState(w, r, true) }

func (h AdminHandler) setUserState(w http.ResponseWriter, r *http.Request, isActive bool) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	item, err := h.Users.SetUserState(r.Context(), userID, isActive)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	action := "disable"
	if isActive {
		action = "enable"
	}
	h.record(r, "admin.user", "user", userID, action, "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": item})
}

func (h AdminHandler) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, err := h.Users.ResetPassword(r.Context(), userID, body.Password)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "user", userID, "reset_password", "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": item})
}

func (h AdminHandler) ListUserIdentities(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	items, err := h.Users.ListUserIdentities(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) DeleteUserIdentity(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	identityID := strings.TrimSpace(chi.URLParam(r, "identityID"))
	if err := h.Users.DeleteUserIdentity(r.Context(), userID, identityID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "user_identity", identityID, "unlink_identity", "success", map[string]any{"user_id": userID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AdminHandler) ListUserAppKeys(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	items, err := h.Users.ListAppKeys(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) RevokeUserAppKey(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if err := h.Users.RevokeAppKey(r.Context(), userID, keyID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "app_api_key", keyID, "revoke_app_key", "success", map[string]any{"user_id": userID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AdminHandler) ListUserModelKeys(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	items, err := h.Users.ListModelKeys(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) RevokeUserModelKey(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if err := h.Users.RevokeModelKey(r.Context(), userID, keyID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "model_api_key", keyID, "revoke_model_key", "success", map[string]any{"user_id": userID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AdminHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	preview, err := h.Users.DeletePreview(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "preview": preview})
}

func (h AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	if err := h.Users.DeleteUser(r.Context(), userID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "user", userID, "delete", "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AdminHandler) TransferOwnership(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	var body struct {
		TargetUserID string `json:"target_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, err := h.Users.TransferOwnership(r.Context(), userID, body.TargetUserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "user", userID, "transfer_ownership", "success", map[string]any{"target_user_id": body.TargetUserID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (h AdminHandler) OwnershipItems(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	items, err := h.Users.OwnershipItems(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h AdminHandler) TransferOwnershipSelection(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	var body struct {
		TargetUserID    string   `json:"target_user_id"`
		ConversationIDs []string `json:"conversation_ids"`
		Filenames       []string `json:"filenames"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, err := h.Users.TransferOwnershipSelection(r.Context(), userID, body.TargetUserID, body.ConversationIDs, body.Filenames)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "user", userID, "transfer_ownership_selection", "success", map[string]any{"target_user_id": body.TargetUserID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (h AdminHandler) ListRequests(w http.ResponseWriter, r *http.Request) {
	items, err := h.Chat.ListRequests(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) GetRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	item, err := h.Chat.GetRequest(r.Context(), requestID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "request": item})
}

func (h AdminHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	items, err := h.Chat.ListConversations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	items, err := h.Chat.ListMessages(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) AbortConversation(w http.ResponseWriter, r *http.Request) {
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, err := h.Chat.AbortConversation(r.Context(), conversationID, body.Reason)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.request", "conversation", conversationID, "abort", "success", nil)
	writeJSON(w, http.StatusOK, result)
}

func (h AdminHandler) CompleteConversation(w http.ResponseWriter, r *http.Request) {
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	var body struct {
		Text       string `json:"text"`
		Mode       string `json:"mode"`
		ToolName   string `json:"tool_name"`
		ToolCallID string `json:"tool_call_id"`
		ToolOutput string `json:"tool_output"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, err := h.Chat.CompleteConversation(r.Context(), conversationID, body.Text, body.Mode, body.ToolName, body.ToolCallID, body.ToolOutput)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.request", "conversation", conversationID, "complete", "success", nil)
	writeJSON(w, http.StatusOK, result)
}

func (h AdminHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	result, err := h.Chat.DeleteConversation(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.request", "conversation", conversationID, "delete", "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (h AdminHandler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			limit = value
		}
	}
	items, err := h.Audit.List(r.Context(), auditsvc.ListInput{
		Limit:         limit,
		EventType:     r.URL.Query().Get("event_type"),
		ActorUserID:   r.URL.Query().Get("actor_user_id"),
		IncludeAppAPI: parseBoolQuery(r.URL.Query().Get("include_app_api")),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) GetAuthSettings(w http.ResponseWriter, r *http.Request) {
	if h.AuthSettings == nil {
		http.Error(w, "auth settings service not configured", http.StatusInternalServerError)
		return
	}
	item, err := h.AuthSettings.Get(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item["ok"] = true
	writeJSON(w, http.StatusOK, item)
}

func (h AdminHandler) SetAuthSettings(w http.ResponseWriter, r *http.Request) {
	if h.AuthSettings == nil {
		http.Error(w, "auth settings service not configured", http.StatusInternalServerError)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, err := h.AuthSettings.Set(r.Context(), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.record(r, "admin.auth", "auth_settings", "system_settings", "update", "success", nil)
	item["ok"] = true
	writeJSON(w, http.StatusOK, item)
}

func (h AdminHandler) GetAccessSettings(w http.ResponseWriter, r *http.Request) {
	if h.AccessSettings == nil {
		http.Error(w, "access settings service not configured", http.StatusInternalServerError)
		return
	}
	item, err := h.AccessSettings.GetDocument(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h AdminHandler) SetAccessSettings(w http.ResponseWriter, r *http.Request) {
	if h.AccessSettings == nil {
		http.Error(w, "access settings service not configured", http.StatusInternalServerError)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, err := h.AccessSettings.Set(r.Context(), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.record(r, "admin.access", "access_settings", "system_access_settings", "update", "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"key":              "system_access_settings",
		"current":          item,
		"schema":           h.AccessSettings.Schema(),
		"response_version": 1,
	})
}

func (h AdminHandler) record(r *http.Request, eventType string, resourceType string, resourceID string, action string, outcome string, metadata map[string]any) {
	if h.Audit == nil {
		return
	}
	act, _ := actor.FromContext(r.Context())
	_, _ = h.Audit.RecordActor(r.Context(), act, eventType, resourceType, resourceID, action, outcome, metadata)
	logging.BindContext(h.Logger, r.Context(),
		zap.String("event_type", eventType),
		zap.String("resource_type", resourceType),
		zap.String("resource_id", resourceID),
	).Debug("admin audit recorded")
}

func statusForStoreError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, store.ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, store.ErrTurnConflict) {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}

func parseBoolQuery(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
