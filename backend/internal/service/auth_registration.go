package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/platform/password"
	"github.com/zyf/chatapi/internal/store"
)

var ErrRegistrationDisabled = errors.New("registration is disabled")
var ErrPasswordResetDisabled = errors.New("password reset is disabled")

type RegistrationService struct {
	store    store.Store
	settings *AuthSettingsService
	codes    *EmailCodeService
}

func NewRegistrationService(dataStore store.Store, settings *AuthSettingsService, codes *EmailCodeService) *RegistrationService {
	return &RegistrationService{store: dataStore, settings: settings, codes: codes}
}

func (s *RegistrationService) SendCode(ctx context.Context, email string) error {
	settings, err := s.settings.Public(ctx)
	if err != nil {
		return err
	}
	if !settings.RegistrationEnabled || !settings.EmailVerificationEnabled {
		return ErrRegistrationDisabled
	}
	email = normalizeEmail(email)
	if err := s.settings.ValidateRegistrationEmail(email, settings); err != nil {
		return err
	}
	return s.codes.SendCode(ctx, email, PurposeRegister(), codeSubject(PurposeRegister()), codeBodyPrefix(PurposeRegister()))
}

func (s *RegistrationService) Register(ctx context.Context, email string, plainPassword string, code string) (store.User, error) {
	settings, err := s.settings.Public(ctx)
	if err != nil {
		return store.User{}, err
	}
	if !settings.RegistrationEnabled {
		return store.User{}, ErrRegistrationDisabled
	}
	email = normalizeEmail(email)
	if email == "" || strings.TrimSpace(plainPassword) == "" {
		return store.User{}, ErrInvalidUserInput
	}
	if err := s.settings.ValidateRegistrationEmail(email, settings); err != nil {
		return store.User{}, err
	}
	if settings.EmailVerificationEnabled {
		if err := s.codes.VerifyCode(ctx, email, PurposeRegister(), code); err != nil {
			return store.User{}, err
		}
	}
	if _, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return store.User{}, errors.New("email is already registered")
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.User{}, err
	}
	hash, err := password.Hash(plainPassword)
	if err != nil {
		return store.User{}, err
	}
	return s.store.CreateUser(ctx, store.CreateUserInput{
		ID:           "user_" + uuid.NewString(),
		Username:     email,
		Email:        email,
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
		LocalAdmin:   false,
	})
}

type PasswordResetService struct {
	store    store.Store
	settings *AuthSettingsService
	codes    *EmailCodeService
}

func NewPasswordResetService(dataStore store.Store, settings *AuthSettingsService, codes *EmailCodeService) *PasswordResetService {
	return &PasswordResetService{store: dataStore, settings: settings, codes: codes}
}

func (s *PasswordResetService) SendCode(ctx context.Context, email string) error {
	settings, err := s.settings.Public(ctx)
	if err != nil {
		return err
	}
	if !settings.PasswordResetEnabled {
		return ErrPasswordResetDisabled
	}
	email = normalizeEmail(email)
	if email == "" {
		return ErrInvalidUserInput
	}
	if _, err := s.store.GetUserByEmail(ctx, email); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	return s.codes.SendCode(ctx, email, PurposePasswordReset(), codeSubject(PurposePasswordReset()), codeBodyPrefix(PurposePasswordReset()))
}

func (s *PasswordResetService) Reset(ctx context.Context, email string, code string, plainPassword string) error {
	settings, err := s.settings.Public(ctx)
	if err != nil {
		return err
	}
	if !settings.PasswordResetEnabled {
		return ErrPasswordResetDisabled
	}
	email = normalizeEmail(email)
	if email == "" || strings.TrimSpace(plainPassword) == "" {
		return ErrInvalidUserInput
	}
	if err := s.codes.VerifyCode(ctx, email, PurposePasswordReset(), code); err != nil {
		return err
	}
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return err
	}
	hash, err := password.Hash(plainPassword)
	if err != nil {
		return err
	}
	_, err = s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: hash,
		Role:         normalizeUserRole(user.Role),
		IsActive:     user.IsActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  user.LastLoginAt,
	})
	return err
}
