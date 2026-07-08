package local

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/platform/password"
	auditrepo "github.com/zyf/chatapi/internal/repository/audit"
	"github.com/zyf/chatapi/internal/repository/common"
	"github.com/zyf/chatapi/internal/service/account"
	"github.com/zyf/chatapi/internal/service/auth/authn/verification"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	"github.com/zyf/chatapi/internal/service/auth/authz/principal"
	"github.com/zyf/chatapi/internal/service/auth/authz/session"
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
	accounts     *account.Service
	auditStore   auditrepo.Store
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
	User      common.User         `json:"user"`
	Principal principal.Principal `json:"principal"`
	Claims    session.Claims      `json:"claims"`
}

func NewService(accounts *account.Service, auditStore auditrepo.Store, policies *policy.Service, sessions *session.Service, verificationService *verification.Service) *Service {
	return &Service{
		accounts:     accounts,
		auditStore:   auditStore,
		policies:     policies,
		sessions:     sessions,
		verification: verificationService,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (common.User, error) {
	username := strings.TrimSpace(input.Username)
	emailAddress := strings.ToLower(strings.TrimSpace(input.Email))
	passwordText := strings.TrimSpace(input.Password)
	if username == "" {
		return common.User{}, ErrUsernameRequired
	}
	if emailAddress == "" {
		return common.User{}, ErrEmailRequired
	}
	if passwordText == "" {
		return common.User{}, ErrPasswordRequired
	}
	if s.verification != nil && strings.TrimSpace(input.VerificationCode) != "" {
		if err := s.verification.VerifyCode(ctx, emailAddress, verification.PurposeRegister, input.VerificationCode); err != nil {
			return common.User{}, err
		}
	}

	if _, err := s.accounts.GetUserByEmail(ctx, emailAddress); err == nil {
		return common.User{}, ErrUserExists
	} else if !errors.Is(err, common.ErrNotFound) {
		return common.User{}, err
	}
	if _, err := s.accounts.GetUserByUsername(ctx, username); err == nil {
		return common.User{}, ErrUserExists
	} else if !errors.Is(err, common.ErrNotFound) {
		return common.User{}, err
	}
	user, err := s.accounts.CreateUser(ctx, account.CreateUserInput{
		ID:       "user_" + uuid.NewString(),
		Username: username,
		Email:    emailAddress,
		Password: passwordText,
		Role:     "user",
		IsActive: true,
	})
	if err != nil {
		if errors.Is(err, account.ErrUserExists) {
			return common.User{}, ErrUserExists
		}
		return common.User{}, err
	}
	_, _ = s.auditStore.CreateAuditLog(ctx, common.CreateAuditLogInput{
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
	user, err := s.accounts.LookupUserByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
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
	update := common.UpdateUserInput{
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
	user, err = s.accounts.UpdateUser(ctx, account.UpdateUserInput{
		ID:           update.ID,
		Username:     update.Username,
		Email:        update.Email,
		PasswordHash: update.PasswordHash,
		Role:         update.Role,
		IsActive:     update.IsActive,
		LocalAdmin:   update.LocalAdmin,
		LastLoginAt:  update.LastLoginAt,
	})
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
	_, _ = s.auditStore.CreateAuditLog(ctx, common.CreateAuditLogInput{
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
	if _, err := s.accounts.GetUserByEmail(ctx, emailAddress); err != nil {
		if errors.Is(err, common.ErrNotFound) {
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
	user, err := s.accounts.GetUserByEmail(ctx, emailAddress)
	if err != nil {
		return err
	}
	_, err = s.accounts.SetPassword(ctx, user.ID, input.NewPassword)
	if err != nil {
		return err
	}
	_, _ = s.auditStore.CreateAuditLog(ctx, common.CreateAuditLogInput{
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
	user, err := s.accounts.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	_, err = s.accounts.SetPassword(ctx, user.ID, newPassword)
	if err != nil {
		return err
	}
	_, _ = s.auditStore.CreateAuditLog(ctx, common.CreateAuditLogInput{
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
