package oidc

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

var (
	ErrAccessDenied     = errors.New("oidc access denied")
	ErrUserNotFound     = errors.New("oidc user not found")
	ErrEmailUnverified  = errors.New("oidc email is not verified")
	ErrSubjectMissing   = errors.New("oidc subject is required")
	ErrIdentityConflict = errors.New("oidc identity is already linked to another user")
)

type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	PreferredName string
	Profile       map[string]any
}

type Service struct {
	store store.Store
	cfg   config.Config
	now   func() time.Time
}

type AuthResult struct {
	User         store.User
	Identity     store.UserIdentity
	PreviousRole string
	RoleChanged  bool
}

func NewService(dataStore store.Store, cfg config.Config) *Service {
	return &Service{
		store: dataStore,
		cfg:   cfg,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) AuthenticateResult(ctx context.Context, claims Claims) (AuthResult, error) {
	if s == nil || s.store == nil {
		return AuthResult{}, ErrUserNotFound
	}
	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return AuthResult{}, ErrSubjectMissing
	}
	email := normalizeEmail(claims.Email)
	if !s.isAllowedEmail(email, claims.EmailVerified) {
		return AuthResult{}, ErrAccessDenied
	}
	identity, err := s.store.GetUserIdentity(ctx, "oidc", subject)
	if err == nil {
		user, err := s.store.GetUser(ctx, identity.UserID)
		if err != nil {
			return AuthResult{}, err
		}
		return s.updateLogin(ctx, user, claims)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return AuthResult{}, err
	}
	user, err := s.lookupOrCreateUser(ctx, claims)
	if err != nil {
		return AuthResult{}, err
	}
	return s.updateLogin(ctx, user, claims)
}

func (s *Service) BindIdentity(ctx context.Context, userID string, claims Claims) (AuthResult, error) {
	if s == nil || s.store == nil {
		return AuthResult{}, ErrUserNotFound
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return AuthResult{}, ErrUserNotFound
	}
	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return AuthResult{}, ErrSubjectMissing
	}
	email := normalizeEmail(claims.Email)
	if !s.isAllowedEmail(email, claims.EmailVerified) {
		return AuthResult{}, ErrAccessDenied
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return AuthResult{}, err
	}
	if !user.IsActive {
		return AuthResult{}, ErrAccessDenied
	}
	identity, err := s.store.GetUserIdentity(ctx, "oidc", subject)
	if err == nil && identity.UserID != user.ID {
		return AuthResult{}, ErrIdentityConflict
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return AuthResult{}, err
	}
	return s.updateUserAndIdentity(ctx, user, identity, claims)
}

func (s *Service) updateLogin(ctx context.Context, user store.User, claims Claims) (AuthResult, error) {
	return s.updateUserAndIdentity(ctx, user, store.UserIdentity{}, claims)
}

func (s *Service) updateUserAndIdentity(ctx context.Context, user store.User, existingIdentity store.UserIdentity, claims Claims) (AuthResult, error) {
	if !user.IsActive {
		return AuthResult{}, ErrAccessDenied
	}
	email := normalizeEmail(claims.Email)
	previousRole := userRole(user)
	lastLoginAt := s.now()
	updated, err := s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     firstNonEmptyString(user.Username, usernameFromClaims(claims)),
		Email:        firstNonEmptyString(user.Email, email),
		PasswordHash: user.PasswordHash,
		Role:         s.nextRole(user, email, claims.EmailVerified),
		IsActive:     user.IsActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  &lastLoginAt,
	})
	if err != nil {
		return AuthResult{}, err
	}
	identity, err := s.store.UpsertUserIdentity(ctx, store.UpsertUserIdentityInput{
		ID:            identityID(existingIdentity),
		UserID:        updated.ID,
		Provider:      "oidc",
		Subject:       strings.TrimSpace(claims.Subject),
		Email:         email,
		EmailVerified: claims.EmailVerified,
		Profile:       claims.Profile,
		LastLoginAt:   &lastLoginAt,
	})
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{
		User:         updated,
		Identity:     identity,
		PreviousRole: previousRole,
		RoleChanged:  previousRole != userRole(updated),
	}, nil
}

func (s *Service) lookupOrCreateUser(ctx context.Context, claims Claims) (store.User, error) {
	email := normalizeEmail(claims.Email)
	if email != "" {
		if !claims.EmailVerified {
			return store.User{}, ErrEmailUnverified
		}
		user, err := s.store.GetUserByEmail(ctx, email)
		if err == nil {
			return user, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return store.User{}, err
		}
	}
	if !s.cfg.OIDCAutoCreateUser {
		return store.User{}, ErrUserNotFound
	}
	return s.store.CreateUser(ctx, store.CreateUserInput{
		ID:           "user_" + uuid.NewString(),
		Username:     usernameFromClaims(claims),
		Email:        email,
		PasswordHash: "",
		Role:         s.roleForEmail(email, claims.EmailVerified),
		IsActive:     true,
		LocalAdmin:   false,
	})
}

func (s *Service) nextRole(user store.User, email string, verified bool) string {
	role := s.roleForEmail(email, verified)
	if user.LocalAdmin {
		return "admin"
	}
	if strings.TrimSpace(role) != "" {
		return role
	}
	if strings.TrimSpace(user.Role) != "" {
		return strings.TrimSpace(user.Role)
	}
	return "user"
}

func (s *Service) isAllowedEmail(email string, verified bool) bool {
	if len(s.cfg.OIDCAllowedEmails) == 0 && len(s.cfg.OIDCAllowedDomains) == 0 {
		return true
	}
	if email == "" || !verified {
		return false
	}
	for _, allowed := range s.cfg.OIDCAllowedEmails {
		if strings.EqualFold(strings.TrimSpace(allowed), email) {
			return true
		}
	}
	_, domain, err := splitEmail(email)
	if err != nil {
		return false
	}
	for _, allowed := range s.cfg.OIDCAllowedDomains {
		if strings.EqualFold(strings.TrimSpace(allowed), domain) {
			return true
		}
	}
	return false
}

func (s *Service) roleForEmail(email string, verified bool) string {
	if email == "" {
		return ""
	}
	for _, adminEmail := range s.cfg.OIDCAdminEmails {
		if strings.EqualFold(strings.TrimSpace(adminEmail), email) && verified {
			return "admin"
		}
	}
	return "user"
}

func identityID(existing store.UserIdentity) string {
	if strings.TrimSpace(existing.ID) != "" {
		return strings.TrimSpace(existing.ID)
	}
	return "identity_" + uuid.NewString()
}

func normalizeEmail(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return ""
	}
	parsed, err := mail.ParseAddress(email)
	if err == nil {
		email = strings.ToLower(parsed.Address)
	}
	return email
}

func splitEmail(email string) (string, string, error) {
	parts := strings.Split(normalizeEmail(email), "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid email")
	}
	return parts[0], parts[1], nil
}

func usernameFromClaims(claims Claims) string {
	for _, value := range []string{claims.PreferredName, claims.Name} {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	local, _, err := splitEmail(claims.Email)
	if err == nil {
		return local
	}
	return "oidc-user"
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func userRole(user store.User) string {
	if user.LocalAdmin || strings.EqualFold(strings.TrimSpace(user.Role), "admin") {
		return "admin"
	}
	if strings.TrimSpace(user.Role) == "" {
		return "user"
	}
	return strings.TrimSpace(user.Role)
}
