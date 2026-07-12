package admincontrol

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/service/account"
)

func (s *Service) ListUsers(ctx context.Context) ([]common.User, error) {
	return s.accounts.ListUsers(ctx)
}

func (s *Service) ListUsersPage(ctx context.Context, page int, pageSize int) (account.UserPage, error) {
	return s.accounts.ListUsersPage(ctx, page, pageSize)
}

func (s *Service) GetUser(ctx context.Context, userID string) (common.User, error) {
	return s.accounts.GetUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) CreateUser(ctx context.Context, actorRole string, input CreateUserInput) (common.User, error) {
	role := strings.ToLower(strings.TrimSpace(input.Role))
	if role == "" {
		role = "user"
	}
	if role != "user" && role != "admin" {
		return common.User{}, errors.New("role must be user or admin")
	}
	if role == "admin" && !strings.EqualFold(strings.TrimSpace(actorRole), "superadmin") {
		return common.User{}, errors.New("only the superadmin can create administrators")
	}
	return s.accounts.CreateUser(ctx, account.CreateUserInput{
		Username:   strings.TrimSpace(input.Username),
		Email:      strings.ToLower(strings.TrimSpace(input.Email)),
		Password:   strings.TrimSpace(input.Password),
		Role:       role,
		IsActive:   input.IsActive,
		LocalAdmin: false,
	})
}

func (s *Service) SetUserState(ctx context.Context, userID string, isActive bool) (common.User, error) {
	return s.accounts.SetUserState(ctx, strings.TrimSpace(userID), isActive)
}

func (s *Service) SetUserRole(ctx context.Context, actorRole string, userID string, role string) (common.User, error) {
	if !strings.EqualFold(strings.TrimSpace(actorRole), "superadmin") {
		return common.User{}, errors.New("only the superadmin can change administrator roles")
	}
	user, err := s.accounts.GetUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return common.User{}, err
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "user" && role != "admin" {
		return common.User{}, errors.New("role must be user or admin")
	}
	if user.LocalAdmin {
		return common.User{}, errors.New("superadmin role is managed by environment configuration")
	}
	return s.accounts.UpdateUser(ctx, account.UpdateUserInput{
		ID: user.ID, Username: user.Username, Email: user.Email, PasswordHash: user.PasswordHash,
		Role: role, IsActive: user.IsActive, LocalAdmin: user.LocalAdmin, LastLoginAt: user.LastLoginAt,
	})
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
