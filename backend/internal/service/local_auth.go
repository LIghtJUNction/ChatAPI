package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/platform/password"
	"github.com/zyf/chatapi/internal/store"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type LocalAuthService struct {
	store store.Store
	now   func() time.Time
}

func NewLocalAuthService(dataStore store.Store) *LocalAuthService {
	return &LocalAuthService{
		store: dataStore,
		now:   time.Now,
	}
}

func (s *LocalAuthService) Authenticate(ctx context.Context, username string, plainPassword string) (RequestActor, error) {
	if s == nil || s.store == nil {
		return RequestActor{}, ErrInvalidCredentials
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return RequestActor{}, ErrInvalidCredentials
	}
	user, err := s.lookupUser(ctx, username)
	if err != nil {
		return RequestActor{}, ErrInvalidCredentials
	}
	if !user.IsActive || strings.TrimSpace(user.PasswordHash) == "" {
		return RequestActor{}, ErrInvalidCredentials
	}
	result, err := password.Verify(plainPassword, user.PasswordHash)
	if err != nil || !result.OK {
		return RequestActor{}, ErrInvalidCredentials
	}

	passwordHash := user.PasswordHash
	if result.NeedsUpgrade {
		passwordHash, err = password.Hash(plainPassword)
		if err != nil {
			return RequestActor{}, err
		}
	}
	lastLoginAt := s.now().UTC()
	if _, err := s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: passwordHash,
		Role:         userRole(user),
		IsActive:     user.IsActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  &lastLoginAt,
	}); err != nil {
		return RequestActor{}, err
	}

	return RequestActor{
		UserID:   user.ID,
		Username: actorUsername(user),
		Role:     userRole(user),
		Source:   "session",
	}, nil
}

func (s *LocalAuthService) lookupUser(ctx context.Context, username string) (store.User, error) {
	user, err := s.store.GetUserByUsername(ctx, username)
	if err == nil {
		return user, nil
	}
	if strings.Contains(username, "@") {
		user, emailErr := s.store.GetUserByEmail(ctx, username)
		if emailErr == nil {
			return user, nil
		}
	}
	return store.User{}, err
}

func actorUsername(user store.User) string {
	for _, value := range []string{user.Username, user.Email, user.ID} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "user"
}

func userRole(user store.User) string {
	if strings.TrimSpace(user.Role) == "" {
		return "user"
	}
	return strings.TrimSpace(user.Role)
}
