package identity

import (
	"context"

	"github.com/zyf/chatapi/internal/service/account"
	"github.com/zyf/chatapi/internal/store"
)

type Service struct {
	accounts *account.Service
}

func NewService(accounts *account.Service) *Service {
	return &Service{accounts: accounts}
}

func (s *Service) GetUser(ctx context.Context, userID string) (store.User, error) {
	return s.accounts.GetUser(ctx, userID)
}

func (s *Service) GetUserByEmail(ctx context.Context, email string) (store.User, error) {
	return s.accounts.GetUserByEmail(ctx, email)
}

func (s *Service) GetUserByUsername(ctx context.Context, username string) (store.User, error) {
	return s.accounts.GetUserByUsername(ctx, username)
}

func (s *Service) ResolveIdentity(ctx context.Context, provider string, subject string) (store.User, store.UserIdentity, error) {
	return s.accounts.ResolveIdentity(ctx, provider, subject)
}

func (s *Service) UpsertIdentity(ctx context.Context, input store.UpsertUserIdentityInput) (store.UserIdentity, error) {
	return s.accounts.UpsertIdentity(ctx, input)
}

func (s *Service) ListUserIdentities(ctx context.Context, userID string) ([]store.UserIdentity, error) {
	return s.accounts.ListUserIdentities(ctx, userID)
}

func (s *Service) DeleteUserIdentity(ctx context.Context, identityID string, userID string) error {
	return s.accounts.DeleteUserIdentity(ctx, identityID, userID)
}
