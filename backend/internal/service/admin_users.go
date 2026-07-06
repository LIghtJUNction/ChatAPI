package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/platform/password"
	"github.com/zyf/chatapi/internal/store"
)

var ErrInvalidUserInput = errors.New("invalid user input")

type AdminUserService struct {
	store store.Store
}

type CreateAdminUserInput struct {
	Username string
	Email    string
	Password string
	Role     string
	IsActive *bool
}

type ResetUserPasswordInput struct {
	UserID   string
	Password string
}

func NewAdminUserService(dataStore store.Store) *AdminUserService {
	return &AdminUserService{store: dataStore}
}

func (s *AdminUserService) List(ctx context.Context) ([]store.User, error) {
	if s == nil || s.store == nil {
		return nil, ErrInvalidUserInput
	}
	return s.store.ListUsers(ctx)
}

func (s *AdminUserService) Create(ctx context.Context, input CreateAdminUserInput) (store.User, error) {
	if s == nil || s.store == nil {
		return store.User{}, ErrInvalidUserInput
	}
	username := strings.TrimSpace(input.Username)
	email := strings.TrimSpace(input.Email)
	if username == "" {
		return store.User{}, ErrInvalidUserInput
	}
	if strings.TrimSpace(input.Password) == "" {
		return store.User{}, ErrInvalidUserInput
	}
	role := normalizeUserRole(input.Role)
	hash, err := password.Hash(input.Password)
	if err != nil {
		return store.User{}, err
	}
	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}
	return s.store.CreateUser(ctx, store.CreateUserInput{
		ID:           "user_" + uuid.NewString(),
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		Role:         role,
		IsActive:     isActive,
		LocalAdmin:   false,
	})
}

func (s *AdminUserService) ResetPassword(ctx context.Context, input ResetUserPasswordInput) (store.User, error) {
	if s == nil || s.store == nil {
		return store.User{}, ErrInvalidUserInput
	}
	userID := strings.TrimSpace(input.UserID)
	if userID == "" || strings.TrimSpace(input.Password) == "" {
		return store.User{}, ErrInvalidUserInput
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return store.User{}, err
	}
	hash, err := password.Hash(input.Password)
	if err != nil {
		return store.User{}, err
	}
	return s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: hash,
		Role:         normalizeUserRole(user.Role),
		IsActive:     user.IsActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  user.LastLoginAt,
	})
}

func (s *AdminUserService) Deactivate(ctx context.Context, userID string) (store.User, error) {
	if s == nil || s.store == nil {
		return store.User{}, ErrInvalidUserInput
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return store.User{}, ErrInvalidUserInput
	}
	if actor, ok := RequestActorFromContext(ctx); ok && strings.TrimSpace(actor.UserID) == userID {
		return store.User{}, ErrForbidden
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return store.User{}, err
	}
	return s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		Role:         normalizeUserRole(user.Role),
		IsActive:     false,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  user.LastLoginAt,
	})
}

func normalizeUserRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin":
		return "admin"
	default:
		return "user"
	}
}
