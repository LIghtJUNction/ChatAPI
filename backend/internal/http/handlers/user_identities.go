package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type UserIdentitiesHandler struct {
	Service *service.UserIdentityService
	Audit   *service.AuditService
}

func (h UserIdentitiesHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	if userID == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	identities, err := h.Service.List(r.Context(), userID)
	if err != nil {
		http.Error(w, "failed to list identities", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": identities,
		"count": len(identities),
	})
}

func (h UserIdentitiesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	if userID == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	identityID := strings.TrimSpace(chi.URLParam(r, "identityID"))
	if identityID == "" {
		http.Error(w, "identity id is required", http.StatusBadRequest)
		return
	}
	if err := h.Service.Unlink(r.Context(), userID, identityID); err != nil {
		h.record(r, userID, identityID, "unlink", "failure")
		switch {
		case errors.Is(err, service.ErrLastLoginMethod):
			http.Error(w, "cannot unlink the last login method", http.StatusConflict)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, "identity not found", http.StatusNotFound)
		default:
			http.Error(w, "failed to unlink identity", http.StatusInternalServerError)
		}
		return
	}
	h.record(r, userID, identityID, "unlink", "success")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (h UserIdentitiesHandler) record(r *http.Request, userID string, identityID string, action string, outcome string) {
	if h.Audit == nil {
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "user.identity",
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
