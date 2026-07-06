package service

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
	ErrOIDCAccessDenied     = errors.New("oidc access denied")
	ErrOIDCUserNotFound     = errors.New("oidc user not found")
	ErrOIDCEmailUnverified  = errors.New("oidc email is not verified")
	ErrOIDCSubjectIsMissing = errors.New("oidc subject is required")
	ErrOIDCIdentityConflict = errors.New("oidc identity is already linked to another user")
)

type OIDCClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	PreferredName string
	Profile       map[string]any
}

type OIDCAuthService struct {
	store store.Store
	cfg   config.Config
	now   func() time.Time
}

func NewOIDCAuthService(dataStore store.Store, cfg config.Config) *OIDCAuthService {
	return &OIDCAuthService{
		store: dataStore,
		cfg:   cfg,
		now:   time.Now,
	}
}

func (s *OIDCAuthService) Authenticate(ctx context.Context, claims OIDCClaims) (RequestActor, error) {
	if s == nil || s.store == nil {
		return RequestActor{}, ErrOIDCUserNotFound
	}
	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return RequestActor{}, ErrOIDCSubjectIsMissing
	}
	email := normalizeEmail(claims.Email)
	if !s.isAllowedEmail(email, claims.EmailVerified) {
		return RequestActor{}, ErrOIDCAccessDenied
	}

	identity, err := s.store.GetUserIdentity(ctx, "oidc", subject)
	if err == nil {
		user, err := s.store.GetUser(ctx, identity.UserID)
		if err != nil {
			return RequestActor{}, err
		}
		return s.updateLogin(ctx, user, claims)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return RequestActor{}, err
	}

	user, err := s.lookupOrCreateUser(ctx, claims)
	if err != nil {
		return RequestActor{}, err
	}
	return s.updateLogin(ctx, user, claims)
}

func (s *OIDCAuthService) BindIdentity(ctx context.Context, userID string, claims OIDCClaims) (store.User, store.UserIdentity, error) {
	if s == nil || s.store == nil {
		return store.User{}, store.UserIdentity{}, ErrOIDCUserNotFound
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return store.User{}, store.UserIdentity{}, ErrOIDCUserNotFound
	}
	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return store.User{}, store.UserIdentity{}, ErrOIDCSubjectIsMissing
	}
	email := normalizeEmail(claims.Email)
	if !s.isAllowedEmail(email, claims.EmailVerified) {
		return store.User{}, store.UserIdentity{}, ErrOIDCAccessDenied
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return store.User{}, store.UserIdentity{}, err
	}
	if !user.IsActive {
		return store.User{}, store.UserIdentity{}, ErrOIDCAccessDenied
	}
	identity, err := s.store.GetUserIdentity(ctx, "oidc", subject)
	if err == nil && identity.UserID != user.ID {
		return store.User{}, store.UserIdentity{}, ErrOIDCIdentityConflict
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.User{}, store.UserIdentity{}, err
	}

	lastLoginAt := s.now().UTC()
	updated, err := s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        firstNonEmptyString(user.Email, email),
		PasswordHash: user.PasswordHash,
		Role:         s.nextRole(user, email, claims.EmailVerified),
		IsActive:     user.IsActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  &lastLoginAt,
	})
	if err != nil {
		return store.User{}, store.UserIdentity{}, err
	}
	identity, err = s.store.UpsertUserIdentity(ctx, store.UpsertUserIdentityInput{
		ID:            identityID(identity),
		UserID:        updated.ID,
		Provider:      "oidc",
		Subject:       subject,
		Email:         email,
		EmailVerified: claims.EmailVerified,
		Profile:       claims.Profile,
		LastLoginAt:   &lastLoginAt,
	})
	if err != nil {
		return store.User{}, store.UserIdentity{}, err
	}
	return updated, identity, nil
}

func (s *OIDCAuthService) lookupOrCreateUser(ctx context.Context, claims OIDCClaims) (store.User, error) {
	email := normalizeEmail(claims.Email)
	if email != "" {
		if !claims.EmailVerified {
			return store.User{}, ErrOIDCEmailUnverified
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
		return store.User{}, ErrOIDCUserNotFound
	}
	userID := "user_" + uuid.NewString()
	username := usernameFromClaims(claims)
	return s.store.CreateUser(ctx, store.CreateUserInput{
		ID:           userID,
		Username:     username,
		Email:        email,
		PasswordHash: "",
		Role:         s.roleForEmail(email, claims.EmailVerified),
		IsActive:     true,
		LocalAdmin:   false,
	})
}

func (s *OIDCAuthService) updateLogin(ctx context.Context, user store.User, claims OIDCClaims) (RequestActor, error) {
	if !user.IsActive {
		return RequestActor{}, ErrOIDCAccessDenied
	}
	email := normalizeEmail(claims.Email)
	lastLoginAt := s.now().UTC()
	updated, err := s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        firstNonEmptyString(user.Email, email),
		PasswordHash: user.PasswordHash,
		Role:         s.nextRole(user, email, claims.EmailVerified),
		IsActive:     user.IsActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  &lastLoginAt,
	})
	if err != nil {
		return RequestActor{}, err
	}
	if _, err := s.store.UpsertUserIdentity(ctx, store.UpsertUserIdentityInput{
		ID:            "identity_" + uuid.NewString(),
		UserID:        updated.ID,
		Provider:      "oidc",
		Subject:       claims.Subject,
		Email:         email,
		EmailVerified: claims.EmailVerified,
		Profile:       claims.Profile,
		LastLoginAt:   &lastLoginAt,
	}); err != nil {
		return RequestActor{}, err
	}
	return RequestActor{
		UserID:   updated.ID,
		Username: actorUsername(updated),
		Role:     userRole(updated),
		Source:   "oidc",
	}, nil
}

func (s *OIDCAuthService) nextRole(user store.User, email string, verified bool) string {
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

func identityID(existing store.UserIdentity) string {
	if strings.TrimSpace(existing.ID) != "" {
		return strings.TrimSpace(existing.ID)
	}
	return "identity_" + uuid.NewString()
}

func (s *OIDCAuthService) isAllowedEmail(email string, verified bool) bool {
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
	_, address, err := splitEmail(email)
	if err != nil {
		return false
	}
	for _, domain := range s.cfg.OIDCAllowedDomains {
		if strings.EqualFold(strings.TrimSpace(domain), address) {
			return true
		}
	}
	return false
}

func (s *OIDCAuthService) roleForEmail(email string, verified bool) string {
	if email == "" {
		return ""
	}
	for _, adminEmail := range s.cfg.OIDCAdminEmails {
		if strings.EqualFold(strings.TrimSpace(adminEmail), email) {
			if verified {
				return "admin"
			}
			return ""
		}
	}
	return "user"
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

func splitEmail(email string) (local string, domain string, err error) {
	parts := strings.Split(normalizeEmail(email), "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid email")
	}
	return parts[0], parts[1], nil
}

func usernameFromClaims(claims OIDCClaims) string {
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
