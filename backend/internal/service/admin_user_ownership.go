package service

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

type AdminUserOwnershipService struct {
	store store.Store
}

func NewAdminUserOwnershipService(dataStore store.Store) *AdminUserOwnershipService {
	return &AdminUserOwnershipService{store: dataStore}
}

func (s *AdminUserOwnershipService) Transfer(ctx context.Context, sourceUserID string, targetUserID string) (store.UserOwnershipTransferResult, store.UserDeletionPreview, error) {
	if s == nil || s.store == nil {
		return store.UserOwnershipTransferResult{}, store.UserDeletionPreview{}, ErrInvalidUserInput
	}
	sourceUserID = strings.TrimSpace(sourceUserID)
	targetUserID = strings.TrimSpace(targetUserID)
	if sourceUserID == "" || targetUserID == "" || sourceUserID == targetUserID {
		return store.UserOwnershipTransferResult{}, store.UserDeletionPreview{}, ErrInvalidUserInput
	}
	if actor, ok := RequestActorFromContext(ctx); ok && strings.TrimSpace(actor.UserID) == sourceUserID {
		return store.UserOwnershipTransferResult{}, store.UserDeletionPreview{}, ErrForbidden
	}
	result, err := s.store.TransferUserOwnership(ctx, sourceUserID, targetUserID)
	if err != nil {
		return store.UserOwnershipTransferResult{}, store.UserDeletionPreview{}, err
	}
	preview, err := s.store.PreviewUserDeletion(ctx, sourceUserID)
	if err != nil {
		return store.UserOwnershipTransferResult{}, store.UserDeletionPreview{}, err
	}
	return result, preview, nil
}
