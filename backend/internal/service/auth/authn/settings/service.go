package settings

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/repository/authrepo"
	"github.com/zyf/chatapi/internal/store"
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
	store authrepo.SettingsStore
	cfg   config.Config
}

func NewService(dataStore authrepo.SettingsStore, cfg config.Config) *Service {
	return &Service{store: dataStore, cfg: cfg}
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
	item, err := s.store.GetSystemConfig(ctx, systemSettingsKey)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return PublicSettings{}, err
	}
	if err == nil {
		out.LocalPasswordLoginEnabled = boolValue(item.Value["local_password_login_enabled"], out.LocalPasswordLoginEnabled)
		out.RegistrationEnabled = boolValue(item.Value["external_registration_enabled"], out.RegistrationEnabled)
		out.EmailVerificationEnabled = boolValue(item.Value["email_verification_enabled"], out.EmailVerificationEnabled)
		out.RegistrationEmailDomainRestriction = boolValue(item.Value["registration_email_domain_restriction_enabled"], out.RegistrationEmailDomainRestriction)
		out.RegistrationEmailDomains = csvValue(item.Value["registration_email_domains"])
		out.PasswordResetEnabled = boolValue(item.Value["password_reset_enabled"], out.PasswordResetEnabled)
		out.GeeTestLoginEnabled = boolValue(item.Value["geetest_login_enabled"], out.GeeTestLoginEnabled) && out.GeeTestEnabled
		out.GeeTestRegisterEnabled = boolValue(item.Value["geetest_register_enabled"], out.GeeTestRegisterEnabled) && out.GeeTestEnabled
		out.GeeTestPasswordResetEnabled = boolValue(item.Value["geetest_password_reset_enabled"], out.GeeTestPasswordResetEnabled) && out.GeeTestEnabled
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context) (map[string]any, error) {
	settings, err := s.Public(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"local_password_login_enabled":                  settings.LocalPasswordLoginEnabled,
		"external_registration_enabled":                 settings.RegistrationEnabled,
		"email_verification_enabled":                    settings.EmailVerificationEnabled,
		"registration_email_domain_restriction_enabled": settings.RegistrationEmailDomainRestriction,
		"registration_email_domains":                    strings.Join(settings.RegistrationEmailDomains, ","),
		"password_reset_enabled":                        settings.PasswordResetEnabled,
		"geetest_enabled":                               settings.GeeTestEnabled,
		"geetest_captcha_id":                            settings.GeeTestCaptchaID,
		"geetest_login_enabled":                         settings.GeeTestLoginEnabled,
		"geetest_register_enabled":                      settings.GeeTestRegisterEnabled,
		"geetest_password_reset_enabled":                settings.GeeTestPasswordResetEnabled,
		"oidc_enabled":                                  settings.OIDCEnabled,
		"oidc_provider_name":                            settings.OIDCProviderName,
		"oidc_admin_emails":                             append([]string(nil), s.cfg.OIDCAdminEmails...),
		"oidc_allowed_emails":                           append([]string(nil), s.cfg.OIDCAllowedEmails...),
		"oidc_allowed_domains":                          append([]string(nil), s.cfg.OIDCAllowedDomains...),
		"oidc_auto_create_user":                         s.cfg.OIDCAutoCreateUser,
	}, nil
}

func (s *Service) Set(ctx context.Context, input map[string]any) (map[string]any, error) {
	if s == nil || s.store == nil {
		return nil, ErrInvalidSettings
	}
	current, err := s.Get(ctx)
	if err != nil {
		return nil, err
	}
	value := map[string]any{
		"local_password_login_enabled":                  boolValue(input["local_password_login_enabled"], boolValue(current["local_password_login_enabled"], true)),
		"external_registration_enabled":                 boolValue(input["external_registration_enabled"], boolValue(current["external_registration_enabled"], false)),
		"email_verification_enabled":                    boolValue(input["email_verification_enabled"], boolValue(current["email_verification_enabled"], false)),
		"registration_email_domain_restriction_enabled": boolValue(input["registration_email_domain_restriction_enabled"], boolValue(current["registration_email_domain_restriction_enabled"], false)),
		"registration_email_domains":                    strings.Join(csvFromAny(input["registration_email_domains"], current["registration_email_domains"]), ","),
		"password_reset_enabled":                        boolValue(input["password_reset_enabled"], boolValue(current["password_reset_enabled"], s.cfg.SMTPEnabled)),
		"geetest_login_enabled":                         boolValue(input["geetest_login_enabled"], boolValue(current["geetest_login_enabled"], false)),
		"geetest_register_enabled":                      boolValue(input["geetest_register_enabled"], boolValue(current["geetest_register_enabled"], false)),
		"geetest_password_reset_enabled":                boolValue(input["geetest_password_reset_enabled"], boolValue(current["geetest_password_reset_enabled"], false)),
	}
	if boolValue(value["registration_email_domain_restriction_enabled"], false) && strings.TrimSpace(value["registration_email_domains"].(string)) == "" {
		return nil, errors.New("registration email domains are required")
	}
	if !geetestConfigured(s.cfg) {
		value["geetest_login_enabled"] = false
		value["geetest_register_enabled"] = false
		value["geetest_password_reset_enabled"] = false
	}
	if !s.cfg.SMTPEnabled {
		value["password_reset_enabled"] = false
	}
	if _, err := s.store.SetSystemConfig(ctx, store.SetSystemConfigInput{
		Key:   systemSettingsKey,
		Value: value,
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx)
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
