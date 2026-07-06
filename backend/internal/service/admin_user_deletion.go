package service

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

type AdminUserDeletionService struct {
	store store.Store
}

func NewAdminUserDeletionService(dataStore store.Store) *AdminUserDeletionService {
	return &AdminUserDeletionService{store: dataStore}
}

func (s *AdminUserDeletionService) Preview(ctx context.Context, userID string) (store.UserDeletionPreview, error) {
	if s == nil || s.store == nil {
		return store.UserDeletionPreview{}, ErrInvalidUserInput
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return store.UserDeletionPreview{}, ErrInvalidUserInput
	}
	return s.store.PreviewUserDeletion(ctx, userID)
}

func (s *AdminUserDeletionService) Delete(ctx context.Context, userID string) (store.UserDeletionPreview, error) {
	if s == nil || s.store == nil {
		return store.UserDeletionPreview{}, ErrInvalidUserInput
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return store.UserDeletionPreview{}, ErrInvalidUserInput
	}
	if actor, ok := RequestActorFromContext(ctx); ok && strings.TrimSpace(actor.UserID) == userID {
		return store.UserDeletionPreview{}, ErrForbidden
	}
	preview, err := s.store.PreviewUserDeletion(ctx, userID)
	if err != nil {
		return store.UserDeletionPreview{}, err
	}
	if !preview.CanDelete {
		return preview, ErrUserDeletionBlocked
	}
	if err := s.store.DeleteUserAccount(ctx, userID); err != nil {
		if errors.Is(err, store.ErrTurnConflict) {
			return preview, ErrUserDeletionBlocked
		}
		return store.UserDeletionPreview{}, err
	}
	return preview, nil
}
