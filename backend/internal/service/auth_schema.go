package service

import "github.com/zyf/chatapi/internal/config"

type AuthSchema struct {
	Capabilities map[string]any        `json:"capabilities"`
	Operations   []AuthOperationSchema `json:"operations"`
}

type AuthOperationSchema struct {
	Name          string              `json:"name"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	Description   string              `json:"description"`
	Fields        []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresActor string              `json:"requires_actor,omitempty"`
	Notes         []string            `json:"notes,omitempty"`
}

func BuildAuthSchema(cfg config.Config, settings AuthPublicSettings) AuthSchema {
	return AuthSchema{
		Capabilities: map[string]any{
			"lab_mode":                    cfg.Mode == config.ModeLab,
			"oidc_enabled":                cfg.Mode != config.ModeLab && cfg.OIDCEnabled,
			"oidc_provider_name":          defaultAuthProviderName(cfg),
			"registration_enabled":        settings.RegistrationEnabled,
			"email_verification_enabled":  settings.EmailVerificationEnabled,
			"password_reset_enabled":      settings.PasswordResetEnabled,
			"geetest_enabled":             settings.GeetestEnabled,
			"session_cookie_name":         SessionCookieName,
			"login_rate_limit_failures":   5,
			"login_rate_limit_window_sec": 60,
			"email_code_max_attempts":     EmailCodeMaxFailedAttempts(),
		},
		Operations: []AuthOperationSchema{
			{
				Name:        "session",
				Method:      "GET",
				Path:        "/api/auth/session",
				Description: "Read the current ChatAPI session summary and public auth capability flags.",
				Notes: []string{
					"The response shape is {authenticated, user, totp_enabled, registration_enabled, geetest_enabled, geetest_captcha_id, current_connection_count, realtime_max_connections_per_user, oidc_enabled, oidc_provider_name}.",
				},
			},
			{
				Name:        "login",
				Method:      "POST",
				Path:        "/api/auth/login",
				Description: "Create a ChatAPI session from a local user or the recovery admin password.",
				Fields: []ConfigFieldSchema{
					{Key: "username", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Username or email. When omitted, the handler falls back to admin."},
					{Key: "password", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Local account password or recovery admin password."},
					{Key: "totp", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Required only when TOTP is enabled for the matched account."},
					{Key: "geetest_params", ValueType: "object", DefaultValue: nil, Public: false, AdminWriteOnly: true, Description: "Required when GeeTest is enabled. Shape: {lot_number, captcha_output, pass_token, gen_time}."},
				},
				Notes: []string{
					"In lab mode, this endpoint returns the injected lab actor instead of creating a real session cookie.",
					"When TOTP is required and missing, the response is 401 with totp_required=true.",
				},
			},
			{
				Name:        "logout",
				Method:      "POST",
				Path:        "/api/auth/logout",
				Description: "Expire the current ChatAPI session cookie.",
				Notes: []string{
					"Serve mode requires a valid same-origin session mutation request.",
				},
			},
			{
				Name:        "register_config",
				Method:      "GET",
				Path:        "/api/auth/register/config",
				Description: "Read public registration flags used by the register form.",
			},
			{
				Name:        "register_send_code",
				Method:      "POST",
				Path:        "/api/auth/register/send-code",
				Description: "Send a verification email for user registration.",
				Fields: []ConfigFieldSchema{
					{Key: "email", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Email address to verify for registration."},
					{Key: "geetest_params", ValueType: "object", DefaultValue: nil, Public: false, AdminWriteOnly: true, Description: "Required when GeeTest is enabled. Shape: {lot_number, captcha_output, pass_token, gen_time}."},
				},
				Notes: []string{
					"Rate limited per email and purpose.",
				},
			},
			{
				Name:        "register",
				Method:      "POST",
				Path:        "/api/auth/register",
				Description: "Create a local user after email code verification.",
				Fields: []ConfigFieldSchema{
					{Key: "email", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Email address used during registration."},
					{Key: "password", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Initial account password."},
					{Key: "code", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Verification code sent to the email address."},
					{Key: "geetest_params", ValueType: "object", DefaultValue: nil, Public: false, AdminWriteOnly: true, Description: "Required when GeeTest is enabled and registration is not using email verification."},
				},
			},
			{
				Name:        "password_config",
				Method:      "GET",
				Path:        "/api/auth/password/config",
				Description: "Read public password reset flags used by the reset form.",
			},
			{
				Name:        "password_send_code",
				Method:      "POST",
				Path:        "/api/auth/password/send-code",
				Description: "Send a password reset verification code.",
				Fields: []ConfigFieldSchema{
					{Key: "email", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Email address of the local user resetting their password."},
					{Key: "geetest_params", ValueType: "object", DefaultValue: nil, Public: false, AdminWriteOnly: true, Description: "Required when GeeTest is enabled. Shape: {lot_number, captcha_output, pass_token, gen_time}."},
				},
			},
			{
				Name:        "password_reset",
				Method:      "POST",
				Path:        "/api/auth/password/reset",
				Description: "Reset a local user's password using the emailed verification code.",
				Fields: []ConfigFieldSchema{
					{Key: "email", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Email address of the local user resetting their password."},
					{Key: "code", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Verification code sent to the email address."},
					{Key: "password", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "New account password."},
				},
			},
			{
				Name:          "totp_setup",
				Method:        "GET",
				Path:          "/api/auth/totp/setup",
				Description:   "Generate a new TOTP secret, otpauth URI and QR image for the current session user.",
				RequiresActor: "session_user",
			},
			{
				Name:          "totp_confirm",
				Method:        "POST",
				Path:          "/api/auth/totp/confirm",
				Description:   "Enable TOTP for the current session user after validating the generated secret and code.",
				RequiresActor: "session_user",
				Fields: []ConfigFieldSchema{
					{Key: "secret", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Secret returned by the setup endpoint."},
					{Key: "code", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Current one-time password generated from the secret."},
				},
			},
			{
				Name:          "totp_reset",
				Method:        "POST",
				Path:          "/api/auth/totp/reset",
				Description:   "Disable TOTP for the current session user.",
				RequiresActor: "session_user",
			},
			{
				Name:        "oidc_config",
				Method:      "GET",
				Path:        "/api/auth/oidc/config",
				Description: "Read public OIDC login capability flags and button metadata.",
			},
			{
				Name:        "oidc_login",
				Method:      "GET",
				Path:        "/api/auth/oidc/login",
				Description: "Start the OIDC authorization code + PKCE flow.",
				Notes: []string{
					"Writes short-lived state, nonce and PKCE verifier cookies under /api/auth/oidc before redirecting to the identity provider.",
				},
			},
			{
				Name:          "oidc_link",
				Method:        "GET",
				Path:          "/api/auth/oidc/link",
				Description:   "Start an OIDC authorization code + PKCE flow for linking the provider account to the current session user.",
				RequiresActor: "session_user",
				Notes: []string{
					"Writes short-lived state, nonce, PKCE verifier and intent cookies under /api/auth/oidc before redirecting to the identity provider.",
				},
			},
			{
				Name:        "oidc_callback",
				Method:      "GET",
				Path:        "/api/auth/oidc/callback",
				Description: "Complete the OIDC authorization code flow and either create a ChatAPI session or bind the provider identity to the current session user.",
				Notes: []string{
					"Consumes the state, nonce, PKCE and intent cookies written by the login/link endpoint.",
				},
			},
		},
	}
}

func defaultAuthProviderName(cfg config.Config) string {
	if cfg.OIDCProviderName != "" {
		return cfg.OIDCProviderName
	}
	return "OIDC"
}
