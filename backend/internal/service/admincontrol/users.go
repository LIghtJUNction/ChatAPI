package admincontrol

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/service/account"
	"github.com/zyf/chatapi/internal/store"
)

func (s *Service) ListUsers(ctx context.Context) ([]store.User, error) {
	return s.accounts.ListUsers(ctx)
}

func (s *Service) GetUser(ctx context.Context, userID string) (store.User, error) {
	return s.accounts.GetUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) CreateUser(ctx context.Context, input CreateUserInput) (store.User, error) {
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "user"
	}
	return s.accounts.CreateUser(ctx, account.CreateUserInput{
		Username:   strings.TrimSpace(input.Username),
		Email:      strings.ToLower(strings.TrimSpace(input.Email)),
		Password:   strings.TrimSpace(input.Password),
		Role:       role,
		IsActive:   input.IsActive,
		LocalAdmin: input.LocalAdmin,
	})
}

func (s *Service) SetUserState(ctx context.Context, userID string, isActive bool) (store.User, error) {
	return s.accounts.SetUserState(ctx, strings.TrimSpace(userID), isActive)
}

func (s *Service) ResetPassword(ctx context.Context, userID string, newPassword string) (store.User, error) {
	return s.accounts.SetPassword(ctx, strings.TrimSpace(userID), newPassword)
}

func (s *Service) ListUserIdentities(ctx context.Context, userID string) ([]store.UserIdentity, error) {
	return s.accounts.ListUserIdentities(ctx, strings.TrimSpace(userID))
}

func (s *Service) DeleteUserIdentity(ctx context.Context, userID string, identityID string) error {
	return s.accounts.DeleteUserIdentity(ctx, strings.TrimSpace(identityID), strings.TrimSpace(userID))
}

func (s *Service) DeletePreview(ctx context.Context, userID string) (store.UserDeletionPreview, error) {
	return s.accounts.PreviewDeletion(ctx, strings.TrimSpace(userID))
}

func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	return s.accounts.DeleteUser(ctx, strings.TrimSpace(userID))
}
