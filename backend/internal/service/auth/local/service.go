package local

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/platform/password"
	"github.com/zyf/chatapi/internal/service/auth/policy"
	"github.com/zyf/chatapi/internal/service/auth/principal"
	"github.com/zyf/chatapi/internal/service/auth/session"
	"github.com/zyf/chatapi/internal/service/auth/verification"
	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

var (
	ErrEmailRequired       = errors.New("email is required")
	ErrUsernameRequired    = errors.New("username is required")
	ErrPasswordRequired    = errors.New("password is required")
	ErrNewPasswordRequired = errors.New("new password is required")
	ErrUserExists          = errors.New("user already exists")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrUserDisabled        = errors.New("user is disabled")
	ErrVerificationNeeded  = errors.New("verification code is required")
)

type Service struct {
	store        store.Store
	policies     *policy.Service
	sessions     *session.Service
	verification *verification.Service
	now          func() time.Time
	Logger       *zap.Logger
}

type RegisterInput struct {
	Username         string
	Email            string
	Password         string
	VerificationCode string
}

type LoginInput struct {
	Identifier string
	Password   string
}

type ResetPasswordInput struct {
	Email            string
	VerificationCode string
	NewPassword      string
}

type AuthResult struct {
	User      store.User          `json:"user"`
	Principal principal.Principal `json:"principal"`
	Claims    session.Claims      `json:"claims"`
}

func NewService(dataStore store.Store, policies *policy.Service, sessions *session.Service, verificationService *verification.Service) *Service {
	return &Service{
		store:        dataStore,
		policies:     policies,
		sessions:     sessions,
		verification: verificationService,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (store.User, error) {
	username := strings.TrimSpace(input.Username)
	emailAddress := strings.ToLower(strings.TrimSpace(input.Email))
	passwordText := strings.TrimSpace(input.Password)
	if username == "" {
		return store.User{}, ErrUsernameRequired
	}
	if emailAddress == "" {
		return store.User{}, ErrEmailRequired
	}
	if passwordText == "" {
		return store.User{}, ErrPasswordRequired
	}
	if s.verification != nil && strings.TrimSpace(input.VerificationCode) != "" {
		if err := s.verification.VerifyCode(ctx, emailAddress, verification.PurposeRegister, input.VerificationCode); err != nil {
			return store.User{}, err
		}
	}

	if _, err := s.store.GetUserByEmail(ctx, emailAddress); err == nil {
		return store.User{}, ErrUserExists
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.User{}, err
	}
	if _, err := s.store.GetUserByUsername(ctx, username); err == nil {
		return store.User{}, ErrUserExists
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.User{}, err
	}

	hashed, err := password.Hash(passwordText)
	if err != nil {
		return store.User{}, err
	}
	user, err := s.store.CreateUser(ctx, store.CreateUserInput{
		ID:           "user_" + uuid.NewString(),
		Username:     username,
		Email:        emailAddress,
		PasswordHash: hashed,
		Role:         "user",
		IsActive:     true,
	})
	if err != nil {
		return store.User{}, err
	}
	_, _ = s.store.CreateAuditLog(ctx, store.CreateAuditLogInput{
		ID:           "audit_" + uuid.NewString(),
		ActorUserID:  user.ID,
		ActorRole:    user.Role,
		ActorSource:  "local_register",
		EventType:    "auth.register",
		ResourceType: "user",
		ResourceID:   user.ID,
		Action:       "register",
		Outcome:      "success",
		Metadata: map[string]any{
			"email":    user.Email,
			"username": user.Username,
		},
	})
	return user, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (AuthResult, error) {
	if s.sessions == nil {
		return AuthResult{}, session.ErrMissingSecret
	}
	identifier := strings.TrimSpace(input.Identifier)
	passwordText := strings.TrimSpace(input.Password)
	if identifier == "" || passwordText == "" {
		return AuthResult{}, ErrInvalidCredentials
	}
	user, err := s.lookupUser(ctx, identifier)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return AuthResult{}, ErrInvalidCredentials
		}
		return AuthResult{}, err
	}
	if !user.IsActive {
		return AuthResult{}, ErrUserDisabled
	}
	result, err := password.Verify(passwordText, user.PasswordHash)
	if err != nil || !result.OK {
		return AuthResult{}, ErrInvalidCredentials
	}
	now := s.now()
	update := store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		Role:         user.Role,
		IsActive:     user.IsActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  &now,
	}
	if result.NeedsUpgrade {
		hashed, hashErr := password.Hash(passwordText)
		if hashErr != nil {
			return AuthResult{}, hashErr
		}
		update.PasswordHash = hashed
	}
	user, err = s.store.UpdateUser(ctx, update)
	if err != nil {
		return AuthResult{}, err
	}
	sessionID, err := s.sessions.NewSessionID()
	if err != nil {
		return AuthResult{}, err
	}
	pr := s.policies.SessionPrincipal(user, sessionID, "password")
	claims, _, err := s.sessions.IssueToken(pr)
	if err != nil {
		return AuthResult{}, err
	}
	_, _ = s.store.CreateAuditLog(ctx, store.CreateAuditLogInput{
		ID:           "audit_" + uuid.NewString(),
		ActorUserID:  user.ID,
		ActorRole:    pr.Role,
		ActorSource:  "session",
		EventType:    "auth.login",
		ResourceType: "user",
		ResourceID:   user.ID,
		Action:       "login",
		Outcome:      "success",
		Metadata: map[string]any{
			"auth_method": "password",
			"session_id":  sessionID,
		},
	})
	logging.BindContext(s.Logger, ctx,
		zap.String("auth.kind", "local"),
		zap.String("user.id", user.ID),
	).Info("local login succeeded")
	return AuthResult{User: user, Principal: pr, Claims: claims}, nil
}

func (s *Service) SendPasswordReset(ctx context.Context, emailAddress string) (verification.SendResult, error) {
	if s.verification == nil {
		return verification.SendResult{}, verification.ErrDeliveryDisabled
	}
	emailAddress = strings.ToLower(strings.TrimSpace(emailAddress))
	if emailAddress == "" {
		return verification.SendResult{}, ErrEmailRequired
	}
	if _, err := s.store.GetUserByEmail(ctx, emailAddress); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return verification.SendResult{}, ErrInvalidCredentials
		}
		return verification.SendResult{}, err
	}
	return s.verification.SendCode(ctx, emailAddress, verification.PurposePasswordReset)
}

func (s *Service) ResetPassword(ctx context.Context, input ResetPasswordInput) error {
	emailAddress := strings.ToLower(strings.TrimSpace(input.Email))
	if emailAddress == "" {
		return ErrEmailRequired
	}
	if strings.TrimSpace(input.NewPassword) == "" {
		return ErrNewPasswordRequired
	}
	if strings.TrimSpace(input.VerificationCode) == "" {
		return ErrVerificationNeeded
	}
	if s.verification == nil {
		return verification.ErrDeliveryDisabled
	}
	if err := s.verification.VerifyCode(ctx, emailAddress, verification.PurposePasswordReset, input.VerificationCode); err != nil {
		return err
	}
	user, err := s.store.GetUserByEmail(ctx, emailAddress)
	if err != nil {
		return err
	}
	hashed, err := password.Hash(strings.TrimSpace(input.NewPassword))
	if err != nil {
		return err
	}
	_, err = s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: hashed,
		Role:         user.Role,
		IsActive:     user.IsActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  user.LastLoginAt,
	})
	if err != nil {
		return err
	}
	_, _ = s.store.CreateAuditLog(ctx, store.CreateAuditLogInput{
		ID:           "audit_" + uuid.NewString(),
		ActorUserID:  user.ID,
		ActorRole:    user.Role,
		ActorSource:  "password_reset",
		EventType:    "auth.password_reset",
		ResourceType: "user",
		ResourceID:   user.ID,
		Action:       "reset_password",
		Outcome:      "success",
	})
	return nil
}

func (s *Service) UpdatePasswordForUser(ctx context.Context, userID string, newPassword string) error {
	userID = strings.TrimSpace(userID)
	newPassword = strings.TrimSpace(newPassword)
	if userID == "" {
		return ErrInvalidCredentials
	}
	if newPassword == "" {
		return ErrNewPasswordRequired
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	hashed, err := password.Hash(newPassword)
	if err != nil {
		return err
	}
	_, err = s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: hashed,
		Role:         user.Role,
		IsActive:     user.IsActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  user.LastLoginAt,
	})
	if err != nil {
		return err
	}
	_, _ = s.store.CreateAuditLog(ctx, store.CreateAuditLogInput{
		ID:           "audit_" + uuid.NewString(),
		ActorUserID:  user.ID,
		ActorRole:    user.Role,
		ActorSource:  "session",
		EventType:    "auth.password_change",
		ResourceType: "user",
		ResourceID:   user.ID,
		Action:       "change_password",
		Outcome:      "success",
	})
	return nil
}

func (s *Service) lookupUser(ctx context.Context, identifier string) (store.User, error) {
	if strings.Contains(identifier, "@") {
		return s.store.GetUserByEmail(ctx, strings.ToLower(identifier))
	}
	return s.store.GetUserByUsername(ctx, identifier)
}
