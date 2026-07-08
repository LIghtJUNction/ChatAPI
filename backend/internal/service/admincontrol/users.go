package admincontrol

import (
	"context"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/service/account"
)

func (s *Service) ListUsers(ctx context.Context) ([]common.User, error) {
	return s.accounts.ListUsers(ctx)
}

func (s *Service) GetUser(ctx context.Context, userID string) (common.User, error) {
	return s.accounts.GetUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) CreateUser(ctx context.Context, input CreateUserInput) (common.User, error) {
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

func (s *Service) SetUserState(ctx context.Context, userID string, isActive bool) (common.User, error) {
	return s.accounts.SetUserState(ctx, strings.TrimSpace(userID), isActive)
}

func (s *Service) ResetPassword(ctx context.Context, userID string, newPassword string) (common.User, error) {
	return s.accounts.SetPassword(ctx, strings.TrimSpace(userID), newPassword)
}

func (s *Service) ListUserIdentities(ctx context.Context, userID string) ([]common.UserIdentity, error) {
	return s.accounts.ListUserIdentities(ctx, strings.TrimSpace(userID))
}

func (s *Service) DeleteUserIdentity(ctx context.Context, userID string, identityID string) error {
	return s.accounts.DeleteUserIdentity(ctx, strings.TrimSpace(identityID), strings.TrimSpace(userID))
}

func (s *Service) DeletePreview(ctx context.Context, userID string) (common.UserDeletionPreview, error) {
	return s.accounts.PreviewDeletion(ctx, strings.TrimSpace(userID))
}

func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	return s.accounts.DeleteUser(ctx, strings.TrimSpace(userID))
}
