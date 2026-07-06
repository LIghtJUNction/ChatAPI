package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
	"golang.org/x/oauth2"
)

type AuthHandler struct {
	Config       config.Config
	Audit        *service.AuditService
	LocalAuth    *service.LocalAuthService
	OIDCAuth     *service.OIDCAuthService
	TOTP         *service.TOTPService
	Settings     *service.AuthSettingsService
	Registration *service.RegistrationService
	Passwords    *service.PasswordResetService
	LoginLimiter *service.LoginRateLimiter
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
		OIDCEnabled                   bool   `json:"oidc_enabled"`
		OIDCProviderName              string `json:"oidc_provider_name"`
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
		OIDCEnabled:                   h.Config.Mode != config.ModeLab && h.Config.OIDCEnabled,
		OIDCProviderName:              h.oidcProviderName(),
	}
	if h.Settings != nil {
		if settings, err := h.Settings.Public(r.Context()); err == nil {
			payload.RegistrationEnabled = settings.RegistrationEnabled
		}
	}
	if actor, ok := service.RequestActorFromContext(r.Context()); ok {
		payload.Authenticated = true
		payload.User = &user{ID: actor.UserID, Username: actor.Username, Role: actor.Role}
		if h.TOTP != nil {
			payload.TOTPEnabled = h.TOTP.IsEnabled(r.Context(), actor.UserID)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func (h AuthHandler) RegisterConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Settings.Public(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                         true,
		"registration_enabled":       settings.RegistrationEnabled,
		"email_verification_enabled": settings.EmailVerificationEnabled,
		"registration_email_domain_restriction_enabled": settings.RegistrationEmailDomainRestriction,
		"registration_email_domains":                    settings.RegistrationEmailDomains,
		"geetest_enabled":                               settings.GeetestEnabled,
		"geetest_captcha_id":                            settings.GeetestCaptchaID,
	})
}

func (h AuthHandler) RegisterSendCode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid register send-code request", http.StatusBadRequest)
		return
	}
	if err := h.Registration.SendCode(r.Context(), input.Email); err != nil {
		writeRegistrationError(w, err)
		return
	}
	h.recordAuthAudit(r, "register_send_code", "success")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid register request", http.StatusBadRequest)
		return
	}
	user, err := h.Registration.Register(r.Context(), input.Email, input.Password, input.Code)
	if err != nil {
		writeRegistrationError(w, err)
		return
	}
	h.recordAuthAudit(r.WithContext(service.WithRequestActor(r.Context(), service.RequestActor{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		Source:   "registration",
	})), "register", "success")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"user": map[string]any{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

func (h AuthHandler) PasswordConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Settings.Public(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                     true,
		"password_reset_enabled": settings.PasswordResetEnabled,
		"geetest_enabled":        settings.GeetestEnabled,
		"geetest_captcha_id":     settings.GeetestCaptchaID,
	})
}

func (h AuthHandler) PasswordSendCode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid password send-code request", http.StatusBadRequest)
		return
	}
	if err := h.Passwords.SendCode(r.Context(), input.Email); err != nil {
		writePasswordResetError(w, err)
		return
	}
	h.recordAuthAudit(r, "password_send_code", "success")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) PasswordReset(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid password reset request", http.StatusBadRequest)
		return
	}
	if err := h.Passwords.Reset(r.Context(), input.Email, input.Code, input.Password); err != nil {
		writePasswordResetError(w, err)
		return
	}
	h.recordAuthAudit(r, "password_reset", "success")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) OIDCConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"enabled":       h.Config.Mode != config.ModeLab && h.Config.OIDCEnabled,
		"provider_name": h.oidcProviderName(),
		"login_url":     "/api/auth/oidc/login",
	})
}

func (h AuthHandler) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	if h.Config.Mode == config.ModeLab || !h.Config.OIDCEnabled {
		http.Error(w, "oidc is not enabled", http.StatusNotFound)
		return
	}
	oauthCfg, _, err := h.oidcRuntime(r.Context())
	if err != nil {
		h.recordAuthAudit(r, "oidc_login", "failure")
		http.Error(w, "oidc is not configured", http.StatusInternalServerError)
		return
	}
	state, err := randomToken()
	if err != nil {
		http.Error(w, "failed to create oidc state", http.StatusInternalServerError)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		http.Error(w, "failed to create oidc nonce", http.StatusInternalServerError)
		return
	}
	pkceVerifier := oauth2.GenerateVerifier()
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_state", state, int((10*time.Minute).Seconds())))
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_nonce", nonce, int((10*time.Minute).Seconds())))
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_pkce", pkceVerifier, int((10*time.Minute).Seconds())))
	http.Redirect(w, r, oauthCfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(pkceVerifier)), http.StatusFound)
}

func (h AuthHandler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if h.Config.Mode == config.ModeLab || !h.Config.OIDCEnabled {
		http.Error(w, "oidc is not enabled", http.StatusNotFound)
		return
	}
	if errText := strings.TrimSpace(r.URL.Query().Get("error")); errText != "" {
		h.recordAuthAudit(r, "oidc_callback", "failure")
		http.Error(w, errText, http.StatusUnauthorized)
		return
	}
	stateCookie, err := r.Cookie("chatapi_oidc_state")
	if err != nil || stateCookie.Value == "" || !subtleCompare(stateCookie.Value, r.URL.Query().Get("state")) {
		h.recordAuthAudit(r, "oidc_state", "failure")
		http.Error(w, "invalid oidc state", http.StatusUnauthorized)
		return
	}
	nonceCookie, err := r.Cookie("chatapi_oidc_nonce")
	if err != nil || nonceCookie.Value == "" {
		h.recordAuthAudit(r, "oidc_nonce", "failure")
		http.Error(w, "invalid oidc nonce", http.StatusUnauthorized)
		return
	}
	pkceCookie, err := r.Cookie("chatapi_oidc_pkce")
	if err != nil || pkceCookie.Value == "" {
		h.recordAuthAudit(r, "oidc_pkce", "failure")
		http.Error(w, "invalid oidc pkce verifier", http.StatusUnauthorized)
		return
	}
	oauthCfg, provider, err := h.oidcRuntime(r.Context())
	if err != nil {
		h.recordAuthAudit(r, "oidc_callback", "failure")
		http.Error(w, "oidc is not configured", http.StatusInternalServerError)
		return
	}
	pkceVerifier, err := url.QueryUnescape(pkceCookie.Value)
	if err != nil {
		h.recordAuthAudit(r, "oidc_pkce", "failure")
		http.Error(w, "invalid oidc pkce verifier", http.StatusUnauthorized)
		return
	}
	token, err := oauthCfg.Exchange(r.Context(), strings.TrimSpace(r.URL.Query().Get("code")), oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		h.recordAuthAudit(r, "oidc_exchange", "failure")
		http.Error(w, "oidc token exchange failed", http.StatusUnauthorized)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(rawIDToken) == "" {
		h.recordAuthAudit(r, "oidc_id_token", "failure")
		http.Error(w, "oidc id_token is missing", http.StatusUnauthorized)
		return
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: h.Config.OIDCClientID})
	idToken, err := verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		h.recordAuthAudit(r, "oidc_verify", "failure")
		http.Error(w, "oidc id_token verification failed", http.StatusUnauthorized)
		return
	}
	if idToken.Nonce != nonceCookie.Value {
		h.recordAuthAudit(r, "oidc_nonce", "failure")
		http.Error(w, "invalid oidc nonce", http.StatusUnauthorized)
		return
	}
	var rawClaims map[string]any
	if err := idToken.Claims(&rawClaims); err != nil {
		h.recordAuthAudit(r, "oidc_claims", "failure")
		http.Error(w, "oidc claims are invalid", http.StatusUnauthorized)
		return
	}
	if err := h.mergeUserInfoClaims(r.Context(), provider, token, rawClaims); err != nil {
		h.recordAuthAudit(r, "oidc_userinfo", "failure")
		http.Error(w, "oidc userinfo claims are invalid", http.StatusUnauthorized)
		return
	}
	claims := claimsFromMap(rawClaims)
	if h.OIDCAuth == nil {
		http.Error(w, "oidc auth service is not configured", http.StatusInternalServerError)
		return
	}
	actor, err := h.OIDCAuth.Authenticate(r.Context(), claims)
	if err != nil {
		h.recordAuthAudit(r, oidcFailureAction(err), "failure")
		http.Error(w, oidcFailureMessage(err), http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_state", "", -1))
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_nonce", "", -1))
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_pkce", "", -1))
	h.writeLoginSuccess(w, r, actor, "")
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
		TOTP     string `json:"totp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid login request", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(input.Username)
	if username == "" {
		username = "admin"
	}
	limitKey := loginLimitKey(username, r)
	if h.LoginLimiter != nil && !h.LoginLimiter.Allow(limitKey) {
		h.recordAuthAudit(r, "login_rate_limited", "failure")
		http.Error(w, "too many failed login attempts", http.StatusTooManyRequests)
		return
	}
	if actor, err := h.authenticateLocalUser(r.Context(), username, input.Password); err == nil {
		if h.requireTOTP(w, r, actor, input.TOTP) {
			return
		}
		h.writeLoginSuccess(w, r, actor, limitKey)
		return
	}
	if h.validAdminPassword(username, input.Password) {
		actor := service.RequestActor{
			UserID:   "admin",
			Username: "admin",
			Role:     "admin",
			Source:   "session",
		}
		if h.requireTOTP(w, r, actor, input.TOTP) {
			return
		}
		h.writeLoginSuccess(w, r, actor, limitKey)
		return
	}
	if h.LoginLimiter != nil {
		h.LoginLimiter.RecordFailure(limitKey)
	}
	h.recordAuthAudit(r, "login", "failure")
	http.Error(w, "invalid username or password", http.StatusUnauthorized)
}

func (h AuthHandler) TOTPSetup(w http.ResponseWriter, r *http.Request) {
	actor, ok := service.RequestActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.UserID) == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	setup, err := h.TOTP.Setup(r.Context(), actor.UserID, actor.Username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.recordAuthAudit(r.WithContext(service.WithRequestActor(r.Context(), actor)), "totp_setup", "success")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"secret":    setup.Secret,
		"uri":       setup.URI,
		"qr_base64": setup.QRBase64,
	})
}

func (h AuthHandler) TOTPConfirm(w http.ResponseWriter, r *http.Request) {
	actor, ok := service.RequestActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.UserID) == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	var input struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid totp confirm request", http.StatusBadRequest)
		return
	}
	if err := h.TOTP.Confirm(r.Context(), actor.UserID, input.Secret, input.Code); err != nil {
		switch {
		case errors.Is(err, service.ErrTOTPCodeInvalid), errors.Is(err, service.ErrInvalidTOTPInput), errors.Is(err, service.ErrTOTPNotConfigured):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	h.recordAuthAudit(r.WithContext(service.WithRequestActor(r.Context(), actor)), "totp_confirm", "success")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) TOTPReset(w http.ResponseWriter, r *http.Request) {
	actor, ok := service.RequestActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.UserID) == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	if err := h.TOTP.Reset(r.Context(), actor.UserID); err != nil {
		if errors.Is(err, service.ErrInvalidTOTPInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.recordAuthAudit(r.WithContext(service.WithRequestActor(r.Context(), actor)), "totp_reset", "success")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) requireTOTP(w http.ResponseWriter, r *http.Request, actor service.RequestActor, code string) bool {
	if h.TOTP == nil || !h.TOTP.IsEnabled(r.Context(), actor.UserID) {
		return false
	}
	if err := h.TOTP.ValidateLoginCode(r.Context(), actor.UserID, code); err == nil {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":         "totp code is required",
		"totp_required": true,
	})
	h.recordAuthAudit(r.WithContext(service.WithRequestActor(r.Context(), actor)), "login_totp", "failure")
	return true
}

func (h AuthHandler) authenticateLocalUser(ctx context.Context, username string, password string) (service.RequestActor, error) {
	if h.LocalAuth == nil {
		return service.RequestActor{}, service.ErrInvalidCredentials
	}
	return h.LocalAuth.Authenticate(ctx, username, password)
}

func (h AuthHandler) writeLoginSuccess(w http.ResponseWriter, r *http.Request, actor service.RequestActor, limitKey string) {
	if h.LoginLimiter != nil && limitKey != "" {
		h.LoginLimiter.Reset(limitKey)
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

func (h AuthHandler) oidcRuntime(ctx context.Context) (*oauth2.Config, *oidc.Provider, error) {
	provider, err := oidc.NewProvider(ctx, strings.TrimRight(strings.TrimSpace(h.Config.OIDCIssuerURL), "/"))
	if err != nil {
		return nil, nil, err
	}
	return &oauth2.Config{
		ClientID:     h.Config.OIDCClientID,
		ClientSecret: h.Config.OIDCClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  h.Config.OIDCRedirectURL,
		Scopes:       h.Config.OIDCScopes,
	}, provider, nil
}

func (h AuthHandler) oidcProviderName() string {
	if strings.TrimSpace(h.Config.OIDCProviderName) != "" {
		return strings.TrimSpace(h.Config.OIDCProviderName)
	}
	return "OIDC"
}

func (h AuthHandler) mergeUserInfoClaims(ctx context.Context, provider *oidc.Provider, token *oauth2.Token, rawClaims map[string]any) error {
	if provider == nil || token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return nil
	}
	userInfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		return nil
	}
	var userInfoClaims map[string]any
	if err := userInfo.Claims(&userInfoClaims); err != nil {
		return err
	}
	return mergeOIDCClaims(rawClaims, userInfoClaims)
}

func loginLimitKey(username string, r *http.Request) string {
	return strings.TrimSpace(username) + "|" + directRemoteIP(r)
}

func directRemoteIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
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
	resourceID := "admin"
	if actor, ok := service.RequestActorFromContext(r.Context()); ok && strings.TrimSpace(actor.UserID) != "" {
		resourceID = strings.TrimSpace(actor.UserID)
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "auth.session",
		ResourceType: "session",
		ResourceID:   resourceID,
		Action:       action,
		Outcome:      outcome,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
	})
}

func writeRegistrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrEmailCodeRateLimited):
		http.Error(w, err.Error(), http.StatusTooManyRequests)
	case errors.Is(err, service.ErrRegistrationDisabled),
		errors.Is(err, service.ErrInvalidUserInput),
		errors.Is(err, service.ErrEmailCodeInvalid),
		errors.Is(err, service.ErrEmailCodeExpired),
		errors.Is(err, service.ErrEmailCodeTooManyAttempts),
		errors.Is(err, service.ErrEmailDeliveryUnavailable):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writePasswordResetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrEmailCodeRateLimited):
		http.Error(w, err.Error(), http.StatusTooManyRequests)
	case errors.Is(err, service.ErrPasswordResetDisabled),
		errors.Is(err, service.ErrInvalidUserInput),
		errors.Is(err, service.ErrEmailCodeInvalid),
		errors.Is(err, service.ErrEmailCodeExpired),
		errors.Is(err, service.ErrEmailCodeTooManyAttempts),
		errors.Is(err, service.ErrEmailDeliveryUnavailable):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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

func oidcCookie(r *http.Request, name string, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    url.QueryEscape(value),
		Path:     "/api/auth/oidc",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
}

func randomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func subtleCompare(a string, b string) bool {
	unescaped, err := url.QueryUnescape(a)
	if err == nil {
		a = unescaped
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(strings.TrimSpace(b))) == 1
}

func claimsFromMap(raw map[string]any) service.OIDCClaims {
	return service.OIDCClaims{
		Subject:       stringClaim(raw, "sub"),
		Email:         stringClaim(raw, "email"),
		EmailVerified: boolClaim(raw, "email_verified"),
		Name:          stringClaim(raw, "name"),
		PreferredName: stringClaim(raw, "preferred_username"),
		Profile:       raw,
	}
}

func mergeOIDCClaims(idTokenClaims map[string]any, userInfoClaims map[string]any) error {
	if idTokenClaims == nil || len(userInfoClaims) == 0 {
		return nil
	}
	idSub := stringClaim(idTokenClaims, "sub")
	userInfoSub := stringClaim(userInfoClaims, "sub")
	if idSub != "" && userInfoSub != "" && idSub != userInfoSub {
		return fmt.Errorf("userinfo sub does not match id_token sub")
	}
	for key, value := range userInfoClaims {
		if _, exists := idTokenClaims[key]; exists {
			continue
		}
		idTokenClaims[key] = value
	}
	return nil
}

func stringClaim(raw map[string]any, key string) string {
	if value, ok := raw[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func boolClaim(raw map[string]any, key string) bool {
	if value, ok := raw[key].(bool); ok {
		return value
	}
	return false
}

func oidcFailureAction(err error) string {
	switch {
	case errors.Is(err, service.ErrOIDCAccessDenied):
		return "oidc_access_denied"
	case errors.Is(err, service.ErrOIDCUserNotFound):
		return "oidc_user_not_found"
	case errors.Is(err, service.ErrOIDCEmailUnverified):
		return "oidc_email_unverified"
	default:
		return "oidc_callback"
	}
}

func oidcFailureMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrOIDCAccessDenied):
		return "oidc account is not allowed"
	case errors.Is(err, service.ErrOIDCUserNotFound):
		return "oidc account is not linked"
	default:
		return "oidc login failed"
	}
}
