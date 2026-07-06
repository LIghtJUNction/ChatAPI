package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/service"
)

const (
	kirariStateCookieName = "chatapi_kirari_state"
	kirariNonceCookieName = "chatapi_kirari_nonce"
	kirariPKCECookieName  = "chatapi_kirari_pkce"
	kirariUserCookieName  = "chatapi_kirari_user"
)

type KirariIntegrationHandler struct {
	Config  config.Config
	Service *service.KirariIntegrationService
	Audit   *service.AuditService
}

func (h KirariIntegrationHandler) Schema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"schema": service.BuildKirariIntegrationSchema(),
	})
}

func (h KirariIntegrationHandler) Status(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	if userID == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	status, err := h.Service.Status(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

func (h KirariIntegrationHandler) Connect(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	if userID == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	redirectURL, session, err := h.Service.StartConnect(r.Context(), userID)
	if err != nil {
		h.record(r, userID, "connect_start", "failure", map[string]any{"error": err.Error()})
		writeKirariError(w, err)
		return
	}
	maxAge := int((10 * time.Minute).Seconds())
	http.SetCookie(w, kirariCookie(r, kirariStateCookieName, session.State, maxAge))
	http.SetCookie(w, kirariCookie(r, kirariNonceCookieName, session.Nonce, maxAge))
	http.SetCookie(w, kirariCookie(r, kirariPKCECookieName, session.CodeVerifier, maxAge))
	http.SetCookie(w, kirariCookie(r, kirariUserCookieName, session.UserID, maxAge))
	h.record(r, userID, "connect_start", "success", nil)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (h KirariIntegrationHandler) Callback(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	if userID == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	cookieState, err := kirariCookieValue(r, kirariStateCookieName)
	if err != nil || !subtleCompare(cookieState, state) {
		h.record(r, userID, "connect_complete", "failure", map[string]any{"error": service.ErrKirariInvalidState.Error()})
		http.Error(w, service.ErrKirariInvalidState.Error(), http.StatusBadRequest)
		return
	}
	nonce, err := kirariCookieValue(r, kirariNonceCookieName)
	if err != nil {
		h.record(r, userID, "connect_complete", "failure", map[string]any{"error": service.ErrKirariInvalidState.Error()})
		http.Error(w, service.ErrKirariInvalidState.Error(), http.StatusBadRequest)
		return
	}
	pkceVerifier, err := kirariCookieValue(r, kirariPKCECookieName)
	if err != nil {
		h.record(r, userID, "connect_complete", "failure", map[string]any{"error": service.ErrKirariInvalidState.Error()})
		http.Error(w, service.ErrKirariInvalidState.Error(), http.StatusBadRequest)
		return
	}
	cookieUserID, err := kirariCookieValue(r, kirariUserCookieName)
	if err != nil || strings.TrimSpace(cookieUserID) != userID {
		h.record(r, userID, "connect_complete", "failure", map[string]any{"error": service.ErrKirariInvalidState.Error()})
		http.Error(w, service.ErrKirariInvalidState.Error(), http.StatusBadRequest)
		return
	}
	status, err := h.Service.CompleteConnect(r.Context(), userID, code, service.KirariAuthorizationSession{
		State:        cookieState,
		Nonce:        nonce,
		CodeVerifier: pkceVerifier,
		UserID:       cookieUserID,
	})
	clearKirariCookies(w, r)
	if err != nil {
		h.record(r, userID, "connect_complete", "failure", map[string]any{"error": err.Error()})
		writeKirariError(w, err)
		return
	}
	h.record(r, userID, "connect_complete", "success", map[string]any{
		"subject":        status.Subject,
		"granted_scopes": status.GrantedScopes,
		"expires_at":     status.ExpiresAt,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

func (h KirariIntegrationHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	if userID == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	if err := h.Service.Disconnect(r.Context(), userID); err != nil {
		h.record(r, userID, "disconnect", "failure", map[string]any{"error": err.Error()})
		writeKirariError(w, err)
		return
	}
	clearKirariCookies(w, r)
	h.record(r, userID, "disconnect", "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "disconnected": true})
}

func (h KirariIntegrationHandler) Meta(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	if userID == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	forceRefresh := false
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("force_refresh"))) {
	case "1", "true", "yes", "on":
		forceRefresh = true
	}
	meta, cached, err := h.Service.Meta(r.Context(), userID, forceRefresh)
	if err != nil {
		h.record(r, userID, "meta_refresh", "failure", map[string]any{"error": err.Error(), "force_refresh": forceRefresh})
		writeKirariError(w, err)
		return
	}
	h.record(r, userID, "meta_refresh", "success", map[string]any{"cached": cached, "force_refresh": forceRefresh})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cached": cached, "meta": meta})
}

func (h KirariIntegrationHandler) record(r *http.Request, userID string, action string, outcome string, metadata map[string]any) {
	if h.Audit == nil {
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "user.kirari",
		ResourceType: "kirari_connection",
		ResourceID:   strings.TrimSpace(userID),
		Action:       action,
		Outcome:      outcome,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata:     metadata,
	})
}

func writeKirariError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrKirariDisabled):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrKirariInvalidState), errors.Is(err, service.ErrKirariMissingCode):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, service.ErrKirariNotConnected):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, service.ErrKirariMissingUser):
		http.Error(w, err.Error(), http.StatusUnauthorized)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func kirariCookie(r *http.Request, name string, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    url.QueryEscape(value),
		Path:     "/api/integrations/kirari",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
}

func clearKirariCookies(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, kirariCookie(r, kirariStateCookieName, "", -1))
	http.SetCookie(w, kirariCookie(r, kirariNonceCookieName, "", -1))
	http.SetCookie(w, kirariCookie(r, kirariPKCECookieName, "", -1))
	http.SetCookie(w, kirariCookie(r, kirariUserCookieName, "", -1))
}

func kirariCookieValue(r *http.Request, name string) (string, error) {
	cookie, err := r.Cookie(name)
	if err != nil {
		return "", err
	}
	return url.QueryUnescape(cookie.Value)
}
