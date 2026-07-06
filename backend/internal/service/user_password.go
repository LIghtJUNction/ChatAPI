package service

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/platform/password"
	"github.com/zyf/chatapi/internal/store"
)

type UserPasswordService struct {
	store store.Store
}

func NewUserPasswordService(dataStore store.Store) *UserPasswordService {
	return &UserPasswordService{store: dataStore}
}

func (s *UserPasswordService) UpdatePassword(ctx context.Context, userID string, newPassword string) (store.User, error) {
	if s == nil || s.store == nil {
		return store.User{}, ErrInvalidUserInput
	}
	userID = strings.TrimSpace(userID)
	newPassword = strings.TrimSpace(newPassword)
	if userID == "" || newPassword == "" {
		return store.User{}, ErrInvalidUserInput
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return store.User{}, err
	}
	hash, err := password.Hash(newPassword)
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
