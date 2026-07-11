package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/http/httpx"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/service/admincontrol"
	adminmonitoring "github.com/zyf2007/ChatAPI/internal/service/admincontrol/monitoring"
	adminsettings "github.com/zyf2007/ChatAPI/internal/service/admincontrol/settings"
	auditsvc "github.com/zyf2007/ChatAPI/internal/service/audit"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
	"go.uber.org/zap"
)

type AdminHandler struct {
	Control    *admincontrol.Service
	Timeline   *timelinesvc.Service
	Audit      *auditsvc.Service
	Logger     *zap.Logger
	Monitoring *adminmonitoring.Service
}

func (h AdminHandler) ServeMonitoringStream(w http.ResponseWriter, r *http.Request) {
	if h.Monitoring == nil {
		http.Error(w, "admin monitoring unavailable", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	userIDs := monitoringUserIDs(r.URL.Query().Get("user_ids"), 100)
	for event := range h.Monitoring.Stream(r.Context(), userIDs) {
		payload, err := json.Marshal(event)
		if err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, payload); err != nil {
			return
		}
		flusher.Flush()
	}
}

func monitoringUserIDs(raw string, limit int) []string {
	seen := map[string]struct{}{}
	items := make([]string, 0)
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
		if len(items) == limit {
			break
		}
	}
	return items
}

func (h AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page_size")))
	result, err := h.Control.ListUsersPage(r.Context(), page, pageSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "count": len(result.Items), "items": result.Items,
		"page": result.Page, "page_size": result.PageSize, "total": result.Total,
	})
}

func (h AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	item, err := h.Control.GetUser(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "user": item})
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
	item, err := h.Control.CreateUser(r.Context(), admincontrol.CreateUserInput{
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
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "user": item})
}

func (h AdminHandler) DisableUser(w http.ResponseWriter, r *http.Request) {
	h.setUserState(w, r, false)
}
func (h AdminHandler) EnableUser(w http.ResponseWriter, r *http.Request) { h.setUserState(w, r, true) }

func (h AdminHandler) setUserState(w http.ResponseWriter, r *http.Request, isActive bool) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	item, err := h.Control.SetUserState(r.Context(), userID, isActive)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	action := "disable"
	if isActive {
		action = "enable"
	}
	h.record(r, "admin.user", "user", userID, action, "success", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "user": item})
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
	item, err := h.Control.ResetPassword(r.Context(), userID, body.Password)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "user", userID, "reset_password", "success", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "user": item})
}

func (h AdminHandler) ListUserIdentities(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	items, err := h.Control.ListUserIdentities(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) DeleteUserIdentity(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	identityID := strings.TrimSpace(chi.URLParam(r, "identityID"))
	if err := h.Control.DeleteUserIdentity(r.Context(), userID, identityID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "user_identity", identityID, "unlink_identity", "success", map[string]any{"user_id": userID})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AdminHandler) ListUserAppKeys(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	items, err := h.Control.ListAppKeys(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) RevokeUserAppKey(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if err := h.Control.RevokeAppKey(r.Context(), userID, keyID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "app_api_key", keyID, "revoke_app_key", "success", map[string]any{"user_id": userID})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AdminHandler) ListUserModelKeys(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	items, err := h.Control.ListModelKeys(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) RevokeUserModelKey(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if err := h.Control.RevokeModelKey(r.Context(), userID, keyID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "model_api_key", keyID, "revoke_model_key", "success", map[string]any{"user_id": userID})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AdminHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	preview, err := h.Control.DeletePreview(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "preview": preview})
}

func (h AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	if err := h.Control.DeleteUser(r.Context(), userID); err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "user", userID, "delete", "success", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
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
	result, err := h.Control.TransferOwnership(r.Context(), userID, body.TargetUserID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "user", userID, "transfer_ownership", "success", map[string]any{"target_user_id": body.TargetUserID})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (h AdminHandler) OwnershipItems(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	items, err := h.Control.OwnershipItems(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
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
	result, err := h.Control.TransferOwnershipSelection(r.Context(), userID, body.TargetUserID, body.ConversationIDs, body.Filenames)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.user", "user", userID, "transfer_ownership_selection", "success", map[string]any{"target_user_id": body.TargetUserID})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (h AdminHandler) ListRequests(w http.ResponseWriter, r *http.Request) {
	items, err := h.Control.ListRequests(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) GetRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	item, err := h.Control.GetRequest(r.Context(), requestID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "request": item})
}

func (h AdminHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	items, err := h.Control.ListConversations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	items, err := h.Control.ListMessages(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) ListConversationTimeline(w http.ResponseWriter, r *http.Request) {
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	items, err := h.Timeline.ListTimeline(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
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
	result, err := h.Control.AbortConversation(r.Context(), conversationID, body.Reason)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.request", "conversation", conversationID, "abort", "success", nil)
	httpx.WriteJSON(w, http.StatusOK, result)
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
	result, err := h.Control.CompleteConversation(r.Context(), conversationID, body.Text, body.Mode, body.ToolName, body.ToolCallID, body.ToolOutput)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.request", "conversation", conversationID, "complete", "success", nil)
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h AdminHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	result, err := h.Control.DeleteConversation(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	h.record(r, "admin.request", "conversation", conversationID, "delete", "success", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
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
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

func (h AdminHandler) SettingsCatalog(w http.ResponseWriter, r *http.Request) {
	item, err := h.Control.SettingsCatalog()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "catalog": item})
}
func (h AdminHandler) SettingsOverview(w http.ResponseWriter, r *http.Request) {
	item, err := h.Control.SettingsOverview(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "overview": item})
}
func (h AdminHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	item, err := h.Control.GetSettings(r.Context(), chi.URLParam(r, "domain"))
	if err != nil {
		http.Error(w, err.Error(), statusForStoreError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "document": item})
}
func (h AdminHandler) PatchSettings(w http.ResponseWriter, r *http.Request) {
	var body adminsettings.PatchInput
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, err := h.Control.PatchSettings(r.Context(), chi.URLParam(r, "domain"), body)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": map[string]any{"code": "invalid_settings_patch", "message": err.Error()}})
		return
	}
	h.record(r, "admin.settings", "settings", chi.URLParam(r, "domain"), "update", "success", map[string]any{"applied": result.Applied})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}
func (h AdminHandler) RuntimeSettings(w http.ResponseWriter, r *http.Request) {
	item, err := h.Control.RuntimeSettings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "runtime": item})
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
	if errors.Is(err, common.ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, common.ErrTurnConflict) {
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
