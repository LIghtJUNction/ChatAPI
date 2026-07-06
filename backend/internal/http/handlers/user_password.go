package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type UserPasswordHandler struct {
	Config   config.Config
	Password *service.UserPasswordService
	Audit    *service.AuditService
}

func (h UserPasswordHandler) Post(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid password reset request", http.StatusBadRequest)
		return
	}
	if _, err := h.Password.UpdatePassword(r.Context(), userID, body.Password); err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidUserInput):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if h.Audit != nil {
		h.Audit.Record(r.Context(), service.AuditEventInput{
			EventType:    "user.password",
			ResourceType: "user",
			ResourceID:   userID,
			Action:       "update",
			Outcome:      "success",
			IPAddress:    clientIP(r),
			UserAgent:    r.UserAgent(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
