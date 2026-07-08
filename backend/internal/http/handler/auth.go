package handler

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/zyf/chatapi/internal/http/httpx"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/repository/common"
	auditsvc "github.com/zyf/chatapi/internal/service/audit"
	"github.com/zyf/chatapi/internal/service/auth/authn/geetest"
	localauth "github.com/zyf/chatapi/internal/service/auth/authn/local"
	oidcsvc "github.com/zyf/chatapi/internal/service/auth/authn/oidc"
	"github.com/zyf/chatapi/internal/service/auth/authn/ratelimit"
	authsettings "github.com/zyf/chatapi/internal/service/auth/authn/settings"
	totpsvc "github.com/zyf/chatapi/internal/service/auth/authn/totp"
	"github.com/zyf/chatapi/internal/service/auth/authn/verification"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	"github.com/zyf/chatapi/internal/service/auth/authz/session"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/zyf/chatapi/internal/config"
)

const (
	oidcIntentLogin = "login"
	oidcIntentLink  = "link"
)

type AuthHandler struct {
	Config       config.Config
	LocalAuth    *localauth.Service
	Verification *verification.Service
	Sessions     *session.Service
	Policy       *policy.Service
	Settings     *authsettings.Service
	GeeTest      *geetest.Service
	TOTP         *totpsvc.Service
	OIDC         *oidcsvc.Service
	Audit        *auditsvc.Service
	LoginLimiter *ratelimit.Service
	Logger       *zap.Logger
}

func (h AuthHandler) RegisterConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":                         true,
		"registration_enabled":       settings.RegistrationEnabled,
		"email_verification_enabled": settings.EmailVerificationEnabled,
		"registration_email_domain_restriction_enabled": settings.RegistrationEmailDomainRestriction,
		"registration_email_domains":                    settings.RegistrationEmailDomains,
		"geetest_enabled":                               settings.GeeTestEnabled && settings.GeeTestRegisterEnabled,
		"geetest_captcha_id":                            settings.GeeTestCaptchaID,
	})
}

func (h AuthHandler) RegisterSendCode(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !settings.RegistrationEnabled || !settings.EmailVerificationEnabled {
		http.Error(w, "registration is disabled", http.StatusBadRequest)
		return
	}
	var body struct {
		Email         string         `json:"email"`
		GeeTestParams geetest.Params `json:"geetest_params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if settings.GeeTestRegisterEnabled {
		if err := h.validateGeeTest(r.Context(), body.GeeTestParams); err != nil {
			h.writeAuthError(w, r, err)
			return
		}
	}
	if err := h.validateRegistrationEmail(body.Email, settings); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := h.Verification.SendCode(r.Context(), body.Email, verification.PurposeRegister)
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "verification": result})
}

func (h AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !settings.RegistrationEnabled {
		http.Error(w, "registration is disabled", http.StatusBadRequest)
		return
	}
	var body struct {
		Username         string         `json:"username"`
		Email            string         `json:"email"`
		Password         string         `json:"password"`
		VerificationCode string         `json:"verification_code"`
		Code             string         `json:"code"`
		GeeTestParams    geetest.Params `json:"geetest_params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if err := h.validateRegistrationEmail(body.Email, settings); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	code := firstNonEmpty(body.VerificationCode, body.Code)
	if settings.EmailVerificationEnabled && strings.TrimSpace(code) == "" {
		h.writeAuthError(w, r, localauth.ErrVerificationNeeded)
		return
	}
	if settings.GeeTestRegisterEnabled && !settings.EmailVerificationEnabled {
		if err := h.validateGeeTest(r.Context(), body.GeeTestParams); err != nil {
			h.writeAuthError(w, r, err)
			return
		}
	}
	username := strings.TrimSpace(body.Username)
	if username == "" {
		username = usernameFromEmail(body.Email)
	}
	user, err := h.LocalAuth.Register(r.Context(), localauth.RegisterInput{
		Username:         username,
		Email:            body.Email,
		Password:         body.Password,
		VerificationCode: code,
	})
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	h.recordAuthAudit(r, user.ID, user.Role, "session", "auth.register", "user", user.ID, "register", "success", nil)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "user": sanitizeUser(user)})
}

func (h AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !settings.LocalPasswordLoginEnabled {
		http.Error(w, "local password login is disabled", http.StatusForbidden)
		return
	}
	var body struct {
		Identifier    string         `json:"identifier"`
		Username      string         `json:"username"`
		Password      string         `json:"password"`
		TOTP          string         `json:"totp"`
		GeeTestParams geetest.Params `json:"geetest_params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if settings.GeeTestLoginEnabled {
		if err := h.validateGeeTest(r.Context(), body.GeeTestParams); err != nil {
			h.writeAuthError(w, r, err)
			return
		}
	}
	identifier := firstNonEmpty(body.Identifier, body.Username)
	limitKey := loginLimitKey(identifier, r)
	if h.LoginLimiter != nil && !h.LoginLimiter.Allow(limitKey) {
		http.Error(w, "too many failed login attempts", http.StatusTooManyRequests)
		return
	}
	result, err := h.LocalAuth.Login(r.Context(), localauth.LoginInput{
		Identifier: identifier,
		Password:   body.Password,
	})
	if err != nil {
		if h.LoginLimiter != nil {
			h.LoginLimiter.RecordFailure(limitKey)
		}
		h.writeAuthError(w, r, err)
		return
	}
	if h.requireTOTP(w, r, result.User.ID, body.TOTP) {
		return
	}
	if _, err := h.Sessions.IssueCookie(w, result.Principal); err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	if h.LoginLimiter != nil {
		h.LoginLimiter.Reset(limitKey)
	}
	h.recordAuthAudit(r, result.User.ID, result.Principal.Role, "session", "auth.login", "user", result.User.ID, "login", "success", map[string]any{
		"auth_method": result.Principal.AuthMethod,
	})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"user":      sanitizeUser(result.User),
		"principal": result.Principal,
		"session":   result.Claims,
	})
}

func (h AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if h.Sessions != nil {
		h.Sessions.ClearCookie(w)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) OIDCConfig(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":       h.Config.Mode != config.ModeLab && h.Config.OIDCEnabled,
		"provider_name": h.oidcProviderName(),
		"login_url":     "/api/auth/oidc/login",
		"link_url":      "/api/auth/oidc/link",
	})
}

func (h AuthHandler) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	if h.Config.Mode == config.ModeLab || !h.Config.OIDCEnabled {
		http.Error(w, "oidc is not enabled", http.StatusNotFound)
		return
	}
	oauthCfg, _, err := h.oidcRuntime(r.Context())
	if err != nil {
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
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_intent", oidcIntentLogin, int((10*time.Minute).Seconds())))
	http.Redirect(w, r, oauthCfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(pkceVerifier)), http.StatusFound)
}

func (h AuthHandler) OIDCLink(w http.ResponseWriter, r *http.Request) {
	if h.Config.Mode == config.ModeLab || !h.Config.OIDCEnabled {
		http.Error(w, "oidc is not enabled", http.StatusNotFound)
		return
	}
	principal, ok := session.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	oauthCfg, _, err := h.oidcRuntime(r.Context())
	if err != nil {
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
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_intent", oidcIntentLink, int((10*time.Minute).Seconds())))
	http.Redirect(w, r, oauthCfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(pkceVerifier)), http.StatusFound)
}

func (h AuthHandler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if h.Config.Mode == config.ModeLab || !h.Config.OIDCEnabled {
		http.Error(w, "oidc is not enabled", http.StatusNotFound)
		return
	}
	if errText := strings.TrimSpace(r.URL.Query().Get("error")); errText != "" {
		http.Error(w, errText, http.StatusUnauthorized)
		return
	}
	stateValue, err := oidcCookieValue(r, "chatapi_oidc_state")
	if err != nil || stateValue == "" || !subtleCompare(stateValue, r.URL.Query().Get("state")) {
		http.Error(w, "invalid oidc state", http.StatusUnauthorized)
		return
	}
	nonceValue, err := oidcCookieValue(r, "chatapi_oidc_nonce")
	if err != nil || nonceValue == "" {
		http.Error(w, "invalid oidc nonce", http.StatusUnauthorized)
		return
	}
	pkceVerifier, err := oidcCookieValue(r, "chatapi_oidc_pkce")
	if err != nil || pkceVerifier == "" {
		http.Error(w, "invalid oidc pkce verifier", http.StatusUnauthorized)
		return
	}
	oauthCfg, provider, err := h.oidcRuntime(r.Context())
	if err != nil {
		http.Error(w, "oidc is not configured", http.StatusInternalServerError)
		return
	}
	token, err := oauthCfg.Exchange(r.Context(), strings.TrimSpace(r.URL.Query().Get("code")), oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		http.Error(w, "oidc token exchange failed", http.StatusUnauthorized)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(rawIDToken) == "" {
		http.Error(w, "oidc id_token is missing", http.StatusUnauthorized)
		return
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: h.Config.OIDCClientID})
	idToken, err := verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "oidc id_token verification failed", http.StatusUnauthorized)
		return
	}
	if idToken.Nonce != nonceValue {
		http.Error(w, "invalid oidc nonce", http.StatusUnauthorized)
		return
	}
	var rawClaims map[string]any
	if err := idToken.Claims(&rawClaims); err != nil {
		http.Error(w, "oidc claims are invalid", http.StatusUnauthorized)
		return
	}
	if err := mergeUserInfoClaims(r.Context(), provider, token, rawClaims); err != nil {
		http.Error(w, "oidc userinfo claims are invalid", http.StatusUnauthorized)
		return
	}
	intent := oidcIntentLogin
	if value, err := oidcCookieValue(r, "chatapi_oidc_intent"); err == nil && strings.TrimSpace(value) == oidcIntentLink {
		intent = oidcIntentLink
	}
	claims := claimsFromMap(rawClaims)
	if intent == oidcIntentLink {
		principal, ok := session.PrincipalFromContext(r.Context())
		if !ok || strings.TrimSpace(principal.UserID) == "" {
			http.Error(w, "session required", http.StatusUnauthorized)
			return
		}
		result, err := h.OIDC.BindIdentity(r.Context(), principal.UserID, claims)
		if err != nil {
			http.Error(w, oidcFailureMessage(err), oidcFailureStatus(err))
			return
		}
		clearOIDCCookies(w, r)
		if err := h.issueSessionForUser(w, result.User, "oidc_link"); err != nil {
			http.Error(w, "session is not configured", http.StatusInternalServerError)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"linked":   true,
			"identity": result.Identity,
			"user": map[string]any{
				"id":       result.User.ID,
				"username": result.User.Username,
				"role":     h.Policy.EffectiveRole(result.User),
			},
		})
		return
	}
	result, err := h.OIDC.AuthenticateResult(r.Context(), claims)
	if err != nil {
		http.Error(w, oidcFailureMessage(err), oidcFailureStatus(err))
		return
	}
	clearOIDCCookies(w, r)
	if err := h.issueSessionForUser(w, result.User, "oidc"); err != nil {
		http.Error(w, "session is not configured", http.StatusInternalServerError)
		return
	}
	h.recordAuthAudit(r, result.User.ID, h.Policy.EffectiveRole(result.User), "oidc", "auth.login", "user", result.User.ID, "login", "success", map[string]any{
		"provider":     h.oidcProviderName(),
		"identity_id":  result.Identity.ID,
		"identity_sub": result.Identity.Subject,
	})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"user": sanitizeUser(result.User),
	})
}

func (h AuthHandler) PasswordConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":                     true,
		"password_reset_enabled": settings.PasswordResetEnabled,
		"geetest_enabled":        settings.GeeTestEnabled && settings.GeeTestPasswordResetEnabled,
		"geetest_captcha_id":     settings.GeeTestCaptchaID,
	})
}

func (h AuthHandler) PasswordSendCode(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !settings.PasswordResetEnabled {
		http.Error(w, "password reset is disabled", http.StatusBadRequest)
		return
	}
	var body struct {
		Email         string         `json:"email"`
		GeeTestParams geetest.Params `json:"geetest_params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if settings.GeeTestPasswordResetEnabled {
		if err := h.validateGeeTest(r.Context(), body.GeeTestParams); err != nil {
			h.writeAuthError(w, r, err)
			return
		}
	}
	result, err := h.LocalAuth.SendPasswordReset(r.Context(), body.Email)
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "verification": result})
}

func (h AuthHandler) SendVerification(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, err := h.Verification.SendCode(r.Context(), body.Email, body.Purpose)
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "verification": result})
}

func (h AuthHandler) VerifyCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
		Code    string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if err := h.Verification.VerifyCode(r.Context(), body.Email, body.Purpose, body.Code); err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !settings.PasswordResetEnabled {
		http.Error(w, "password reset is disabled", http.StatusBadRequest)
		return
	}
	var body struct {
		Email         string         `json:"email"`
		GeeTestParams geetest.Params `json:"geetest_params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if settings.GeeTestPasswordResetEnabled {
		if err := h.validateGeeTest(r.Context(), body.GeeTestParams); err != nil {
			h.writeAuthError(w, r, err)
			return
		}
	}
	result, err := h.LocalAuth.SendPasswordReset(r.Context(), body.Email)
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "verification": result})
}

func (h AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !settings.PasswordResetEnabled {
		http.Error(w, "password reset is disabled", http.StatusBadRequest)
		return
	}
	var body struct {
		Email            string `json:"email"`
		VerificationCode string `json:"verification_code"`
		Code             string `json:"code"`
		NewPassword      string `json:"new_password"`
		Password         string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if err := h.LocalAuth.ResetPassword(r.Context(), localauth.ResetPasswordInput{
		Email:            body.Email,
		VerificationCode: firstNonEmpty(body.VerificationCode, body.Code),
		NewPassword:      firstNonEmpty(body.NewPassword, body.Password),
	}); err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) TOTPSetup(w http.ResponseWriter, r *http.Request) {
	principal, ok := session.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	setup, err := h.TOTP.Setup(r.Context(), principal.UserID, principal.Username)
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"secret":    setup.Secret,
		"uri":       setup.URI,
		"qr_base64": setup.QRBase64,
	})
}

func (h AuthHandler) TOTPConfirm(w http.ResponseWriter, r *http.Request) {
	principal, ok := session.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	var body struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if err := h.TOTP.Confirm(r.Context(), principal.UserID, body.Secret, body.Code); err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) TOTPReset(w http.ResponseWriter, r *http.Request) {
	principal, ok := session.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	if err := h.TOTP.Reset(r.Context(), principal.UserID); err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) requireTOTP(w http.ResponseWriter, r *http.Request, userID string, code string) bool {
	if h.TOTP == nil || !h.TOTP.IsEnabled(r.Context(), userID) {
		return false
	}
	if err := h.TOTP.ValidateLoginCode(r.Context(), userID, code); err == nil {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":         "totp code is required",
		"totp_required": true,
	})
	return true
}

func (h AuthHandler) issueSessionForUser(w http.ResponseWriter, user any, authMethod string) error {
	if h.Sessions == nil || h.Policy == nil {
		return session.ErrMissingSecret
	}
	storeUser, ok := user.(common.User)
	if !ok {
		return session.ErrInvalidSession
	}
	sessionID, err := h.Sessions.NewSessionID()
	if err != nil {
		return err
	}
	principal := h.Policy.SessionPrincipal(storeUser, sessionID, authMethod)
	_, err = h.Sessions.IssueCookie(w, principal)
	return err
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

func (h AuthHandler) settings(ctx context.Context) (authsettings.PublicSettings, error) {
	if h.Settings == nil {
		return authsettings.PublicSettings{
			LocalPasswordLoginEnabled: true,
			RegistrationEnabled:       true,
			PasswordResetEnabled:      h.Config.SMTPEnabled,
			GeeTestEnabled:            h.GeeTest != nil && h.GeeTest.Enabled(),
			GeeTestCaptchaID:          firstNonEmpty(h.Config.GeetestCaptchaID),
			OIDCEnabled:               h.Config.Mode != config.ModeLab && h.Config.OIDCEnabled,
			OIDCProviderName:          h.oidcProviderName(),
		}, nil
	}
	return h.Settings.Public(ctx)
}

func (h AuthHandler) validateGeeTest(ctx context.Context, params geetest.Params) error {
	if h.GeeTest == nil {
		return nil
	}
	return h.GeeTest.Validate(ctx, params)
}

func (h AuthHandler) recordAuthAudit(r *http.Request, actorUserID string, actorRole string, actorSource string, eventType string, resourceType string, resourceID string, action string, outcome string, metadata map[string]any) {
	if h.Audit == nil {
		return
	}
	_, _ = h.Audit.Record(r.Context(), common.CreateAuditLogInput{
		ActorUserID:  strings.TrimSpace(actorUserID),
		ActorRole:    strings.TrimSpace(actorRole),
		ActorSource:  strings.TrimSpace(actorSource),
		EventType:    strings.TrimSpace(eventType),
		ResourceType: strings.TrimSpace(resourceType),
		ResourceID:   strings.TrimSpace(resourceID),
		Action:       strings.TrimSpace(action),
		Outcome:      strings.TrimSpace(outcome),
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata:     metadata,
	})
}

func (h AuthHandler) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, localauth.ErrInvalidCredentials),
		errors.Is(err, verification.ErrCodeNotFound),
		errors.Is(err, verification.ErrCodeInvalid),
		errors.Is(err, oidcsvc.ErrAccessDenied),
		errors.Is(err, oidcsvc.ErrUserNotFound),
		errors.Is(err, oidcsvc.ErrEmailUnverified),
		errors.Is(err, oidcsvc.ErrSubjectMissing),
		errors.Is(err, totpsvc.ErrCodeInvalid),
		errors.Is(err, geetest.ErrFailed):
		status = http.StatusUnauthorized
	case errors.Is(err, localauth.ErrUserDisabled):
		status = http.StatusForbidden
	case errors.Is(err, localauth.ErrUserExists),
		errors.Is(err, oidcsvc.ErrIdentityConflict):
		status = http.StatusConflict
	case errors.Is(err, verification.ErrCodeRateLimited):
		status = http.StatusTooManyRequests
	case errors.Is(err, verification.ErrDeliveryDisabled),
		errors.Is(err, geetest.ErrUnavailable):
		status = http.StatusServiceUnavailable
	case errors.Is(err, verification.ErrCodeExpired),
		errors.Is(err, verification.ErrCodeAttemptsLimit):
		status = http.StatusGone
	case errors.Is(err, localauth.ErrEmailRequired),
		errors.Is(err, localauth.ErrUsernameRequired),
		errors.Is(err, localauth.ErrPasswordRequired),
		errors.Is(err, localauth.ErrNewPasswordRequired),
		errors.Is(err, localauth.ErrVerificationNeeded),
		errors.Is(err, verification.ErrInvalidPurpose),
		errors.Is(err, verification.ErrCodeRequired),
		errors.Is(err, geetest.ErrRequired),
		errors.Is(err, geetest.ErrInvalidParams),
		errors.Is(err, totpsvc.ErrInvalidInput),
		errors.Is(err, totpsvc.ErrNotConfigured):
		status = http.StatusBadRequest
	}
	logging.BindContext(h.Logger, r.Context(),
		zap.Int("http.status_code", status),
		zap.String("error", strings.TrimSpace(err.Error())),
	).Warn("auth request failed")
	http.Error(w, err.Error(), status)
}

func sanitizeUser(user any) any { return user }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func usernameFromEmail(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	if parts := strings.Split(email, "@"); len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
		return strings.TrimSpace(parts[0])
	}
	return email
}

func loginLimitKey(identifier string, r *http.Request) string {
	return strings.TrimSpace(identifier) + "|" + directRemoteIP(r)
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

func clientIP(r *http.Request) string {
	return directRemoteIP(r)
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

func clearOIDCCookies(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_state", "", -1))
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_nonce", "", -1))
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_pkce", "", -1))
	http.SetCookie(w, oidcCookie(r, "chatapi_oidc_intent", "", -1))
}

func oidcCookieValue(r *http.Request, name string) (string, error) {
	cookie, err := r.Cookie(name)
	if err != nil {
		return "", err
	}
	return url.QueryUnescape(cookie.Value)
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

func claimsFromMap(raw map[string]any) oidcsvc.Claims {
	return oidcsvc.Claims{
		Subject:       stringClaim(raw, "sub"),
		Email:         stringClaim(raw, "email"),
		EmailVerified: boolClaim(raw, "email_verified"),
		Name:          stringClaim(raw, "name"),
		PreferredName: stringClaim(raw, "preferred_username"),
		Profile:       raw,
	}
}

func mergeUserInfoClaims(ctx context.Context, provider *oidc.Provider, token *oauth2.Token, rawClaims map[string]any) error {
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
	if rawClaims == nil || len(userInfoClaims) == 0 {
		return nil
	}
	idSub := stringClaim(rawClaims, "sub")
	userInfoSub := stringClaim(userInfoClaims, "sub")
	if idSub != "" && userInfoSub != "" && idSub != userInfoSub {
		return errors.New("userinfo sub does not match id_token sub")
	}
	for key, value := range userInfoClaims {
		if _, exists := rawClaims[key]; !exists {
			rawClaims[key] = value
		}
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

func oidcFailureMessage(err error) string {
	switch {
	case errors.Is(err, oidcsvc.ErrAccessDenied):
		return "oidc account is not allowed"
	case errors.Is(err, oidcsvc.ErrUserNotFound):
		return "oidc account is not linked"
	case errors.Is(err, oidcsvc.ErrIdentityConflict):
		return "oidc account is already linked to another user"
	case errors.Is(err, oidcsvc.ErrSubjectMissing):
		return "oidc subject is required"
	default:
		return "oidc login failed"
	}
}

func oidcFailureStatus(err error) int {
	switch {
	case errors.Is(err, oidcsvc.ErrIdentityConflict):
		return http.StatusConflict
	case errors.Is(err, oidcsvc.ErrSubjectMissing):
		return http.StatusBadRequest
	default:
		return http.StatusUnauthorized
	}
}

func (h AuthHandler) validateRegistrationEmail(email string, settings authsettings.PublicSettings) error {
	if h.Settings == nil {
		return nil
	}
	return h.Settings.ValidateRegistrationEmail(email, settings)
}
