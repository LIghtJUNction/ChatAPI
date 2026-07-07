package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	localauth "github.com/zyf/chatapi/internal/service/auth/local"
	"github.com/zyf/chatapi/internal/service/auth/session"
	"github.com/zyf/chatapi/internal/service/auth/verification"
	"go.uber.org/zap"
)

type AuthHandler struct {
	LocalAuth    *localauth.Service
	Verification *verification.Service
	Sessions     *session.Service
	Logger       *zap.Logger
}

func (h AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username         string `json:"username"`
		Email            string `json:"email"`
		Password         string `json:"password"`
		VerificationCode string `json:"verification_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	user, err := h.LocalAuth.Register(r.Context(), localauth.RegisterInput{
		Username:         body.Username,
		Email:            body.Email,
		Password:         body.Password,
		VerificationCode: body.VerificationCode,
	})
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"ok":   true,
		"user": sanitizeUser(user),
	})
}

func (h AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Identifier string `json:"identifier"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, err := h.LocalAuth.Login(r.Context(), localauth.LoginInput{
		Identifier: firstNonEmpty(body.Identifier, body.Username),
		Password:   body.Password,
	})
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	if _, err := h.Sessions.IssueCookie(w, result.Principal); err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) Session(w http.ResponseWriter, r *http.Request) {
	pr, ok := middleware.UserSessionPrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authenticated": false})
		return
	}
	claims, _ := session.ClaimsFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"authenticated": true,
		"principal":     pr,
		"session":       claims,
	})
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "verification": result})
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	result, err := h.LocalAuth.SendPasswordReset(r.Context(), body.Email)
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "verification": result})
}

func (h AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email            string `json:"email"`
		VerificationCode string `json:"verification_code"`
		NewPassword      string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if err := h.LocalAuth.ResetPassword(r.Context(), localauth.ResetPasswordInput{
		Email:            body.Email,
		VerificationCode: body.VerificationCode,
		NewPassword:      body.NewPassword,
	}); err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AuthHandler) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, localauth.ErrInvalidCredentials),
		errors.Is(err, verification.ErrCodeNotFound),
		errors.Is(err, verification.ErrCodeInvalid):
		status = http.StatusUnauthorized
	case errors.Is(err, localauth.ErrUserDisabled):
		status = http.StatusForbidden
	case errors.Is(err, localauth.ErrUserExists):
		status = http.StatusConflict
	case errors.Is(err, verification.ErrCodeRateLimited):
		status = http.StatusTooManyRequests
	case errors.Is(err, verification.ErrDeliveryDisabled):
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
		errors.Is(err, verification.ErrCodeRequired):
		status = http.StatusBadRequest
	}
	logging.BindContext(h.Logger, r.Context(),
		zap.Int("http.status_code", status),
		zap.String("error", strings.TrimSpace(err.Error())),
	).Warn("auth request failed")
	http.Error(w, err.Error(), status)
}

func sanitizeUser(user any) any {
	return user
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
