package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

type AuthHandler struct {
	Config config.Config
	Audit  *service.AuditService
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
	if h.Config.Mode == config.ModeLab {
		userPayload := map[string]any{}
		if actor, ok := service.RequestActorFromContext(r.Context()); ok {
			userPayload = map[string]any{"id": actor.UserID, "username": actor.Username, "role": actor.Role}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"user": userPayload,
		})
		return
	}

	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid login request", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(input.Username)
	if username == "" {
		username = "admin"
	}
	if !h.validAdminPassword(username, input.Password) {
		h.recordAuthAudit(r, "login", "failure")
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	actor := service.RequestActor{
		UserID:   "admin",
		Username: "admin",
		Role:     "admin",
		Source:   "session",
	}
	codec := service.NewSessionCodec(h.Config.SessionSecret)
	sessionValue, err := codec.Encode(actor, service.DefaultSessionTTL)
	if err != nil {
		http.Error(w, "session is not configured", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, sessionCookie(r, sessionValue, service.SessionMaxAge(service.DefaultSessionTTL)))
	h.recordAuthAudit(r.WithContext(service.WithRequestActor(r.Context(), actor)), "login", "success")
	userPayload := map[string]any{"id": actor.UserID, "username": actor.Username, "role": actor.Role}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   true,
		"user": userPayload,
	})
}

func (h AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie(r, "", service.ExpiredSessionMaxAge()))
	h.recordAuthAudit(r, "logout", "success")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (h AuthHandler) validAdminPassword(username string, password string) bool {
	if strings.TrimSpace(username) != "admin" {
		return false
	}
	expected := strings.TrimSpace(h.Config.AdminPassword)
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(password), []byte(expected)) == 1
}

func (h AuthHandler) recordAuthAudit(r *http.Request, action string, outcome string) {
	if h.Audit == nil {
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "auth.session",
		ResourceType: "session",
		ResourceID:   "admin",
		Action:       action,
		Outcome:      outcome,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
	})
}

func sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     service.SessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	}
}
