package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

type AuthHandler struct {
	Config config.Config
}

func (h AuthHandler) Session(w http.ResponseWriter, r *http.Request) {
	type user struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	type response struct {
		Authenticated                 bool   `json:"authenticated"`
		User                          *user  `json:"user"`
		TOTPEnabled                   bool   `json:"totp_enabled"`
		RegistrationEnabled           bool   `json:"registration_enabled"`
		GeetestEnabled                bool   `json:"geetest_enabled"`
		GeetestCaptchaID              string `json:"geetest_captcha_id"`
		CurrentConnectionCount        int    `json:"current_connection_count"`
		RealtimeMaxConnectionsPerUser int    `json:"realtime_max_connections_per_user"`
	}

	payload := response{
		Authenticated:                 false,
		User:                          nil,
		TOTPEnabled:                   false,
		RegistrationEnabled:           false,
		GeetestEnabled:                false,
		GeetestCaptchaID:              "",
		CurrentConnectionCount:        0,
		RealtimeMaxConnectionsPerUser: 0,
	}
	if actor, ok := service.RequestActorFromContext(r.Context()); ok {
		payload.Authenticated = true
		payload.User = &user{ID: actor.UserID, Username: actor.Username, Role: actor.Role}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func (h AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	userPayload := map[string]any{}
	if actor, ok := service.RequestActorFromContext(r.Context()); ok {
		userPayload = map[string]any{"id": actor.UserID, "username": actor.Username, "role": actor.Role}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   h.Config.Mode == config.ModeLab,
		"user": userPayload,
	})
}

func (h AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
