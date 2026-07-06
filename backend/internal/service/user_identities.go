package service

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

var ErrLastLoginMethod = errors.New("cannot unlink the last login method")

type UserIdentityService struct {
	store store.Store
}

func NewUserIdentityService(dataStore store.Store) *UserIdentityService {
	return &UserIdentityService{store: dataStore}
}

func (s *UserIdentityService) List(ctx context.Context, userID string) ([]store.UserIdentity, error) {
	if s == nil || s.store == nil {
		return nil, store.ErrNotFound
	}
	return s.store.ListUserIdentities(ctx, strings.TrimSpace(userID))
}

func (s *UserIdentityService) Unlink(ctx context.Context, userID string, identityID string) error {
	if s == nil || s.store == nil {
		return store.ErrNotFound
	}
	userID = strings.TrimSpace(userID)
	identityID = strings.TrimSpace(identityID)
	if userID == "" || identityID == "" {
		return store.ErrNotFound
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	identities, err := s.store.ListUserIdentities(ctx, userID)
	if err != nil {
		return err
	}
	found := false
	for _, identity := range identities {
		if identity.ID == identityID {
			found = true
			break
		}
	}
	if !found {
		return store.ErrNotFound
	}
	if strings.TrimSpace(user.PasswordHash) == "" && len(identities) <= 1 {
		return ErrLastLoginMethod
	}
	return s.store.DeleteUserIdentity(ctx, identityID, userID)
}
