package superadmin

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/service/account"
)

const reservedEmailDomain = "local.superadmin.invalid"

type Service struct {
	accounts *account.Service
	cfg      config.Config
}

func NewService(accounts *account.Service, cfg config.Config) *Service {
	return &Service{accounts: accounts, cfg: cfg}
}

func (s *Service) Sync(ctx context.Context) (common.User, bool, error) {
	if s == nil || s.accounts == nil {
		return common.User{}, false, errors.New("superadmin service unavailable")
	}
	username := strings.TrimSpace(s.cfg.AdminUsername)
	password := strings.TrimSpace(s.cfg.AdminPassword)
	if username == "" || password == "" {
		return s.disableSeedLogin(ctx)
	}
	seed, found, extras, err := s.findSeedUsers(ctx)
	if err != nil {
		return common.User{}, false, err
	}
	email := seedEmail(username)
	if !found {
		created, err := s.accounts.CreateUser(ctx, account.CreateUserInput{
			ID:         "user_" + uuid.NewString(),
			Username:   username,
			Email:      email,
			Password:   password,
			Role:       "admin",
			IsActive:   true,
			LocalAdmin: true,
		})
		if err != nil {
			return common.User{}, false, err
		}
		return created, true, nil
	}
	if err := s.disableExtraSeedUsers(ctx, seed.ID, extras); err != nil {
		return common.User{}, false, err
	}
	if strings.TrimSpace(seed.Username) == username &&
		strings.EqualFold(strings.TrimSpace(seed.Email), email) &&
		seed.IsActive &&
		seed.LocalAdmin &&
		strings.EqualFold(strings.TrimSpace(seed.Role), "admin") {
		updated, err := s.accounts.UpdateUser(ctx, account.UpdateUserInput{
			ID:           seed.ID,
			Username:     seed.Username,
			Email:        seed.Email,
			Password:     password,
			Role:         "admin",
			IsActive:     true,
			LocalAdmin:   true,
			LastLoginAt:  seed.LastLoginAt,
			PasswordHash: seed.PasswordHash,
		})
		return updated, false, err
	}
	updated, err := s.accounts.UpdateUser(ctx, account.UpdateUserInput{
		ID:           seed.ID,
		Username:     username,
		Email:        email,
		Password:     password,
		Role:         "admin",
		IsActive:     true,
		LocalAdmin:   true,
		LastLoginAt:  seed.LastLoginAt,
		PasswordHash: seed.PasswordHash,
	})
	if err != nil {
		return common.User{}, false, err
	}
	return updated, false, nil
}

func (s *Service) disableSeedLogin(ctx context.Context) (common.User, bool, error) {
	seed, found, extras, err := s.findSeedUsers(ctx)
	if err != nil || !found {
		return common.User{}, false, err
	}
	if err := s.disableExtraSeedUsers(ctx, seed.ID, extras); err != nil {
		return common.User{}, false, err
	}
	if !seed.IsActive {
		return seed, false, nil
	}
	updated, err := s.accounts.UpdateUser(ctx, account.UpdateUserInput{
		ID:           seed.ID,
		Username:     seed.Username,
		Email:        seed.Email,
		PasswordHash: seed.PasswordHash,
		Role:         seed.Role,
		IsActive:     false,
		LocalAdmin:   seed.LocalAdmin,
		LastLoginAt:  seed.LastLoginAt,
	})
	if err != nil {
		return common.User{}, false, err
	}
	return updated, false, nil
}

func (s *Service) findSeedUsers(ctx context.Context) (common.User, bool, []common.User, error) {
	users, err := s.accounts.ListUsers(ctx)
	if err != nil {
		return common.User{}, false, nil, err
	}
	var fallback common.User
	var haveFallback bool
	extras := make([]common.User, 0)
	for _, user := range users {
		if !user.LocalAdmin {
			continue
		}
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(user.Email)), "@"+reservedEmailDomain) {
			extras = append(extras, user)
			continue
		}
		if !haveFallback {
			fallback = user
			haveFallback = true
		} else {
			extras = append(extras, user)
		}
	}
	for _, user := range extras {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(user.Email)), "@"+reservedEmailDomain) {
			return user, true, filterUsersByID(extras, user.ID), nil
		}
	}
	if haveFallback {
		return fallback, true, filterUsersByID(extras, fallback.ID), nil
	}
	return common.User{}, false, nil, nil
}

func (s *Service) disableExtraSeedUsers(ctx context.Context, keepID string, users []common.User) error {
	for _, user := range users {
		if strings.TrimSpace(user.ID) == strings.TrimSpace(keepID) {
			continue
		}
		if !user.LocalAdmin {
			continue
		}
		if !user.IsActive {
			continue
		}
		if _, err := s.accounts.UpdateUser(ctx, account.UpdateUserInput{
			ID:           user.ID,
			Username:     user.Username,
			Email:        user.Email,
			PasswordHash: user.PasswordHash,
			Role:         user.Role,
			IsActive:     false,
			LocalAdmin:   user.LocalAdmin,
			LastLoginAt:  user.LastLoginAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func filterUsersByID(users []common.User, excludeID string) []common.User {
	if len(users) == 0 {
		return nil
	}
	out := make([]common.User, 0, len(users))
	for _, user := range users {
		if strings.TrimSpace(user.ID) == strings.TrimSpace(excludeID) {
			continue
		}
		out = append(out, user)
	}
	return out
}

func seedEmail(username string) string {
	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" {
		username = "superadmin"
	}
	return username + "@" + reservedEmailDomain
}
