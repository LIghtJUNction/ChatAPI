package service

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

const systemSettingsKey = "system_settings"

type AuthPublicSettings struct {
	RegistrationEnabled                bool
	EmailVerificationEnabled           bool
	RegistrationEmailDomainRestriction bool
	RegistrationEmailDomains           string
	PasswordResetEnabled               bool
	GeetestEnabled                     bool
	GeetestCaptchaID                   string
}

type AuthSettingsService struct {
	store store.Store
	cfg   config.Config
}

func NewAuthSettingsService(dataStore store.Store, cfg config.Config) *AuthSettingsService {
	return &AuthSettingsService{store: dataStore, cfg: cfg}
}

func (s *AuthSettingsService) Public(ctx context.Context) (AuthPublicSettings, error) {
	if s == nil || s.store == nil {
		return AuthPublicSettings{}, ErrInvalidSystemSettings
	}
	out := AuthPublicSettings{
		RegistrationEnabled:                false,
		EmailVerificationEnabled:           false,
		RegistrationEmailDomainRestriction: false,
		RegistrationEmailDomains:           "",
		PasswordResetEnabled:               s.cfg.SMTPEnabled,
		GeetestEnabled:                     false,
		GeetestCaptchaID:                   "",
	}
	item, err := s.store.GetSystemConfig(ctx, systemSettingsKey)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return AuthPublicSettings{}, err
	}
	if err == nil {
		out.RegistrationEnabled = boolValue(item.Value["external_registration_enabled"], out.RegistrationEnabled)
		out.EmailVerificationEnabled = boolValue(item.Value["email_verification_enabled"], out.EmailVerificationEnabled)
		out.RegistrationEmailDomainRestriction = boolValue(item.Value["registration_email_domain_restriction_enabled"], out.RegistrationEmailDomainRestriction)
		out.RegistrationEmailDomains = stringValue(item.Value["registration_email_domains"], out.RegistrationEmailDomains)
	}
	return out, nil
}

func (s *AuthSettingsService) ValidateRegistrationEmail(email string, settings AuthPublicSettings) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return ErrInvalidUserInput
	}
	if !settings.RegistrationEmailDomainRestriction {
		return nil
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ErrInvalidUserInput
	}
	domain := strings.TrimSpace(parts[1])
	for _, allowed := range strings.Split(settings.RegistrationEmailDomains, ",") {
		if strings.EqualFold(strings.TrimSpace(allowed), domain) {
			return nil
		}
	}
	return errors.New("registration email domain is not allowed")
}
