package identity

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

type Service struct {
	store store.Store
}

func NewService(dataStore store.Store) *Service {
	return &Service{store: dataStore}
}

func (s *Service) GetUser(ctx context.Context, userID string) (store.User, error) {
	return s.store.GetUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) GetUserByEmail(ctx context.Context, email string) (store.User, error) {
	return s.store.GetUserByEmail(ctx, strings.TrimSpace(email))
}

func (s *Service) GetUserByUsername(ctx context.Context, username string) (store.User, error) {
	return s.store.GetUserByUsername(ctx, strings.TrimSpace(username))
}

func (s *Service) ResolveIdentity(ctx context.Context, provider string, subject string) (store.User, store.UserIdentity, error) {
	identity, err := s.store.GetUserIdentity(ctx, strings.TrimSpace(provider), strings.TrimSpace(subject))
	if err != nil {
		return store.User{}, store.UserIdentity{}, err
	}
	user, err := s.store.GetUser(ctx, identity.UserID)
	if err != nil {
		return store.User{}, store.UserIdentity{}, err
	}
	return user, identity, nil
}

func (s *Service) UpsertIdentity(ctx context.Context, input store.UpsertUserIdentityInput) (store.UserIdentity, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.Subject = strings.TrimSpace(input.Subject)
	input.UserID = strings.TrimSpace(input.UserID)
	input.Email = strings.TrimSpace(input.Email)
	return s.store.UpsertUserIdentity(ctx, input)
}

func (s *Service) ListUserIdentities(ctx context.Context, userID string) ([]store.UserIdentity, error) {
	return s.store.ListUserIdentities(ctx, strings.TrimSpace(userID))
}

func (s *Service) DeleteUserIdentity(ctx context.Context, identityID string, userID string) error {
	return s.store.DeleteUserIdentity(ctx, strings.TrimSpace(identityID), strings.TrimSpace(userID))
}
