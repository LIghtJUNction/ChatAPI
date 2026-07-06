package service

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

type AdminUserIdentityService struct {
	store      store.Store
	identities *UserIdentityService
}

func NewAdminUserIdentityService(dataStore store.Store) *AdminUserIdentityService {
	return &AdminUserIdentityService{
		store:      dataStore,
		identities: NewUserIdentityService(dataStore),
	}
}

func (s *AdminUserIdentityService) List(ctx context.Context, userID string) (store.User, []store.UserIdentity, error) {
	if s == nil || s.store == nil {
		return store.User{}, nil, ErrInvalidUserInput
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return store.User{}, nil, ErrInvalidUserInput
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return store.User{}, nil, err
	}
	identities, err := s.store.ListUserIdentities(ctx, userID)
	if err != nil {
		return store.User{}, nil, err
	}
	return user, identities, nil
}

func (s *AdminUserIdentityService) Unlink(ctx context.Context, userID string, identityID string) error {
	if s == nil || s.identities == nil {
		return ErrInvalidUserInput
	}
	userID = strings.TrimSpace(userID)
	identityID = strings.TrimSpace(identityID)
	if userID == "" || identityID == "" {
		return ErrInvalidUserInput
	}
	return s.identities.Unlink(ctx, userID, identityID)
}
