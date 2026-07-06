package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type AdminUsersHandler struct {
	Users      *service.AdminUserService
	History    *service.AdminUserHistoryService
	Identities *service.AdminUserIdentityService
	Deletion   *service.AdminUserDeletionService
	Overview   *service.AdminUserDeleteOverviewService
	Ownership  *service.AdminUserOwnershipService
	Storage    *service.StorageMonitorService
	Audit      *service.AuditService
}

func (h AdminUsersHandler) Schema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"schema": service.BuildAdminUsersSchema(),
	})
}

func (h AdminUsersHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.Users.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"count": len(users),
		"items": users,
		"users": users,
	})
}

func (h AdminUsersHandler) HistoryList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	userID := chi.URLParam(r, "userID")
	user, messages, err := h.History.Get(r.Context(), userID, limit)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"user":            user,
		"recent_messages": messages,
	})
}

func (h AdminUsersHandler) IdentityList(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	user, identities, err := h.Identities.List(r.Context(), userID)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"user":  user,
		"count": len(identities),
		"items": identities,
	})
}

func (h AdminUsersHandler) IdentityDelete(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	identityID := chi.URLParam(r, "identityID")
	if err := h.Identities.Unlink(r.Context(), userID, identityID); err != nil {
		h.recordIdentity(r, userID, identityID, "unlink", "failure")
		writeAdminUserError(w, err)
		return
	}
	h.recordIdentity(r, userID, identityID, "unlink", "success")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AdminUsersHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	preview, err := h.Deletion.Preview(r.Context(), userID)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"user":    preview.User,
		"preview": preview,
	})
}

func (h AdminUsersHandler) DeleteOverview(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	overview, err := h.Overview.Get(r.Context(), userID)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"user":     overview.User,
		"overview": overview,
	})
}

func (h AdminUsersHandler) Purge(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	preview, err := h.Deletion.Delete(r.Context(), userID)
	if err != nil {
		if errors.Is(err, service.ErrUserDeletionBlocked) {
			h.recordWithMetadata(r, userID, "purge", "failure", map[string]any{
				"blockers": preview.Blockers,
				"counts":   preview.Counts,
			})
			writeJSON(w, http.StatusConflict, map[string]any{
				"ok":      false,
				"error":   err.Error(),
				"preview": preview,
			})
			return
		}
		writeAdminUserError(w, err)
		return
	}
	h.recordWithMetadata(r, userID, "purge", "success", map[string]any{
		"counts": preview.Counts,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"deleted": true,
		"preview": preview,
	})
}

func (h AdminUsersHandler) TransferOwnership(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TargetUserID string `json:"target_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid ownership transfer request", http.StatusBadRequest)
		return
	}
	sourceUserID := chi.URLParam(r, "userID")
	result, preview, err := h.Ownership.Transfer(r.Context(), sourceUserID, input.TargetUserID)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	h.recordWithMetadata(r, sourceUserID, "transfer_ownership", "success", map[string]any{
		"target_user_id": strings.TrimSpace(input.TargetUserID),
		"result":         result,
		"preview":        preview,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"result":  result,
		"preview": preview,
	})
}

func (h AdminUsersHandler) OwnershipItems(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	items, err := h.Ownership.Items(r.Context(), userID)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"user":  items.User,
		"items": items,
	})
}

func (h AdminUsersHandler) TransferOwnershipSelection(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TargetUserID    string   `json:"target_user_id"`
		ConversationIDs []string `json:"conversation_ids"`
		Filenames       []string `json:"filenames"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid ownership transfer selection request", http.StatusBadRequest)
		return
	}
	sourceUserID := chi.URLParam(r, "userID")
	result, preview, err := h.Ownership.TransferSelection(r.Context(), sourceUserID, input.TargetUserID, input.ConversationIDs, input.Filenames)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	h.recordWithMetadata(r, sourceUserID, "transfer_ownership_selection", "success", map[string]any{
		"target_user_id":   strings.TrimSpace(input.TargetUserID),
		"conversation_ids": input.ConversationIDs,
		"filenames":        input.Filenames,
		"result":           result,
		"preview":          preview,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"result":  result,
		"preview": preview,
	})
}

func (h AdminUsersHandler) CleanupSelection(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ConversationIDs []string `json:"conversation_ids"`
		Filenames       []string `json:"filenames"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid ownership cleanup selection request", http.StatusBadRequest)
		return
	}
	if len(input.ConversationIDs) == 0 && len(input.Filenames) == 0 {
		http.Error(w, "conversation_ids or filenames is required", http.StatusBadRequest)
		return
	}
	userID := chi.URLParam(r, "userID")
	if h.Storage == nil {
		http.Error(w, "storage monitor is not configured", http.StatusInternalServerError)
		return
	}
	result, err := h.Storage.DeleteOwnershipSelection(r.Context(), userID, input.ConversationIDs, input.Filenames)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	preview, err := h.Deletion.Preview(r.Context(), userID)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	h.recordWithMetadata(r, userID, "cleanup_selection", "success", map[string]any{
		"conversation_ids": input.ConversationIDs,
		"filenames":        input.Filenames,
		"result":           result,
		"preview":          preview,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"result":  result,
		"preview": preview,
	})
}

func (h AdminUsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input service.CreateAdminUserInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid user create request", http.StatusBadRequest)
		return
	}
	user, err := h.Users.Create(r.Context(), input)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	h.record(r, user.ID, "create", "success")
	writeJSON(w, http.StatusCreated, map[string]any{
		"ok":   true,
		"user": user,
	})
}

func (h AdminUsersHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid password reset request", http.StatusBadRequest)
		return
	}
	userID := chi.URLParam(r, "userID")
	user, err := h.Users.ResetPassword(r.Context(), service.ResetUserPasswordInput{
		UserID:   userID,
		Password: input.Password,
	})
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	h.record(r, user.ID, "reset_password", "success")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"user": user,
	})
}

func (h AdminUsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	user, err := h.Users.Deactivate(r.Context(), userID)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	h.record(r, user.ID, "deactivate", "success")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"user": user,
	})
}

func (h AdminUsersHandler) record(r *http.Request, userID string, action string, outcome string) {
	h.recordWithMetadata(r, userID, action, outcome, nil)
}

func (h AdminUsersHandler) recordWithMetadata(r *http.Request, userID string, action string, outcome string, metadata map[string]any) {
	if h.Audit == nil {
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "admin.user",
		ResourceType: "user",
		ResourceID:   userID,
		Action:       action,
		Outcome:      outcome,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata:     metadata,
	})
}

func (h AdminUsersHandler) recordIdentity(r *http.Request, userID string, identityID string, action string, outcome string) {
	if h.Audit == nil {
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "admin.user_identity",
		ResourceType: "user_identity",
		ResourceID:   identityID,
		Action:       action,
		Outcome:      outcome,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"user_id": userID,
		},
	})
}

func writeAdminUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidUserInput):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, service.ErrLastLoginMethod):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, service.ErrUserDeletionBlocked):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
