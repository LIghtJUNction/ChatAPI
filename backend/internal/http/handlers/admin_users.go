package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type AdminUsersHandler struct {
	Users *service.AdminUserService
	Audit *service.AuditService
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
	})
}

func writeAdminUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidUserInput):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
