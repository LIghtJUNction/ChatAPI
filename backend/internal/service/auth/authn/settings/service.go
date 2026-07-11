package settings

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

const systemSettingsKey = "system_settings"

var ErrInvalidSettings = errors.New("invalid auth settings")

type PublicSettings struct {
	LocalPasswordLoginEnabled          bool     `json:"local_password_login_enabled"`
	RegistrationEnabled                bool     `json:"registration_enabled"`
	EmailVerificationEnabled           bool     `json:"email_verification_enabled"`
	RegistrationEmailDomainRestriction bool     `json:"registration_email_domain_restriction_enabled"`
	RegistrationEmailDomains           []string `json:"registration_email_domains"`
	PasswordResetEnabled               bool     `json:"password_reset_enabled"`
	GeeTestEnabled                     bool     `json:"geetest_enabled"`
	GeeTestCaptchaID                   string   `json:"geetest_captcha_id"`
	GeeTestLoginEnabled                bool     `json:"geetest_login_enabled"`
	GeeTestRegisterEnabled             bool     `json:"geetest_register_enabled"`
	GeeTestPasswordResetEnabled        bool     `json:"geetest_password_reset_enabled"`
	OIDCEnabled                        bool     `json:"oidc_enabled"`
	OIDCProviderName                   string   `json:"oidc_provider_name"`
}

type Service struct {
	store auth.SettingsStore
	cfg   config.Config
	core  *settingscore.Service
}

func NewService(dataStore auth.SettingsStore, cfg config.Config) *Service {
	s := &Service{store: dataStore, cfg: cfg}
	s.core = s.newAdminDomain()
	return s
}

func (s *Service) Public(ctx context.Context) (PublicSettings, error) {
	if s == nil || s.store == nil {
		return PublicSettings{}, ErrInvalidSettings
	}
	out := PublicSettings{
		LocalPasswordLoginEnabled:          true,
		RegistrationEnabled:                false,
		EmailVerificationEnabled:           false,
		RegistrationEmailDomainRestriction: false,
		RegistrationEmailDomains:           nil,
		PasswordResetEnabled:               s.cfg.SMTPEnabled,
		GeeTestEnabled:                     geetestConfigured(s.cfg),
		GeeTestCaptchaID:                   strings.TrimSpace(s.cfg.GeetestCaptchaID),
		GeeTestLoginEnabled:                false,
		GeeTestRegisterEnabled:             false,
		GeeTestPasswordResetEnabled:        false,
		OIDCEnabled:                        s.cfg.Mode != config.ModeLab && s.cfg.OIDCEnabled,
		OIDCProviderName:                   providerName(s.cfg),
	}
	doc, err := s.core.Get(ctx)
	if err != nil {
		return PublicSettings{}, err
	}
	out.LocalPasswordLoginEnabled = settingscore.Bool(doc.Values["local_password_login_enabled"])
	out.RegistrationEnabled = settingscore.Bool(doc.Values["external_registration_enabled"])
	out.EmailVerificationEnabled = settingscore.Bool(doc.Values["email_verification_enabled"])
	out.RegistrationEmailDomainRestriction = settingscore.Bool(doc.Values["registration_email_domain_restriction_enabled"])
	out.RegistrationEmailDomains = csvValue(doc.Values["registration_email_domains"])
	out.PasswordResetEnabled = settingscore.Bool(doc.Values["password_reset_enabled"])
	out.GeeTestLoginEnabled = settingscore.Bool(doc.Values["geetest_login_enabled"]) && out.GeeTestEnabled
	out.GeeTestRegisterEnabled = settingscore.Bool(doc.Values["geetest_register_enabled"]) && out.GeeTestEnabled
	out.GeeTestPasswordResetEnabled = settingscore.Bool(doc.Values["geetest_password_reset_enabled"]) && out.GeeTestEnabled
	return out, nil
}

func (s *Service) ValidateRegistrationEmail(email string, settings PublicSettings) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return errors.New("registration email is required")
	}
	if !settings.RegistrationEmailDomainRestriction {
		return nil
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[1] == "" {
		return errors.New("registration email is invalid")
	}
	for _, allowed := range settings.RegistrationEmailDomains {
		if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(parts[1])) {
			return nil
		}
	}
	return errors.New("registration email domain is not allowed")
}

func providerName(cfg config.Config) string {
	if strings.TrimSpace(cfg.OIDCProviderName) != "" {
		return strings.TrimSpace(cfg.OIDCProviderName)
	}
	return "OIDC"
}

func geetestConfigured(cfg config.Config) bool {
	return strings.TrimSpace(cfg.GeetestCaptchaID) != "" && strings.TrimSpace(cfg.GeetestCaptchaKey) != ""
}

func boolValue(value any, fallback bool) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	return fallback
}

func csvValue(value any) []string {
	return splitCSV(stringValue(value, ""))
}

func csvFromAny(value any, fallback any) []string {
	switch raw := value.(type) {
	case string:
		return splitCSV(raw)
	case []string:
		return normalizeStrings(raw)
	case []any:
		items := make([]string, 0, len(raw))
		for _, item := range raw {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return normalizeStrings(items)
	default:
		return csvValue(fallback)
	}
}

func splitCSV(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func normalizeStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stringValue(value any, fallback string) string {
	if typed, ok := value.(string); ok {
		typed = strings.TrimSpace(typed)
		if typed != "" {
			return typed
		}
	}
	return fallback
}
