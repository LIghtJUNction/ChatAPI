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

func (s *AdminUserOwnershipService) Items(ctx context.Context, userID string) (store.UserOwnershipSelection, error) {
	if s == nil || s.store == nil {
		return store.UserOwnershipSelection{}, ErrInvalidUserInput
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return store.UserOwnershipSelection{}, ErrInvalidUserInput
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return store.UserOwnershipSelection{}, err
	}
	conversations, err := s.store.ListConversations(ctx)
	if err != nil {
		return store.UserOwnershipSelection{}, err
	}
	uploads, err := s.store.ListUploadedImagesByOwner(ctx, userID)
	if err != nil {
		return store.UserOwnershipSelection{}, err
	}

	result := store.UserOwnershipSelection{
		User:          user,
		Conversations: make([]store.UserOwnedConversationItem, 0),
		Uploads:       make([]store.UserOwnedUploadItem, 0, len(uploads)),
	}
	for _, conversation := range conversations {
		if stringValue(conversation.Metadata["owner_id"], "") != userID {
			continue
		}
		result.Conversations = append(result.Conversations, store.UserOwnedConversationItem{
			ConversationID: conversation.ID,
			Title:          conversation.Title,
			LastMessageAt:  conversation.LastMessageAt,
			MessageCount:   conversation.MessageCount,
		})
	}
	for _, upload := range uploads {
		result.Uploads = append(result.Uploads, store.UserOwnedUploadItem{
			ID:        upload.ID,
			Filename:  upload.Filename,
			Bytes:     upload.Bytes,
			CreatedAt: upload.CreatedAt,
		})
	}
	return result, nil
}

func (s *AdminUserOwnershipService) TransferSelection(ctx context.Context, sourceUserID string, targetUserID string, conversationIDs []string, filenames []string) (store.UserOwnershipTransferResult, store.UserDeletionPreview, error) {
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
	if len(conversationIDs) == 0 && len(filenames) == 0 {
		return store.UserOwnershipTransferResult{}, store.UserDeletionPreview{}, ErrInvalidUserInput
	}
	result, err := s.store.TransferUserOwnershipSelection(ctx, sourceUserID, targetUserID, conversationIDs, filenames)
	if err != nil {
		return store.UserOwnershipTransferResult{}, store.UserDeletionPreview{}, err
	}
	preview, err := s.store.PreviewUserDeletion(ctx, sourceUserID)
	if err != nil {
		return store.UserOwnershipTransferResult{}, store.UserDeletionPreview{}, err
	}
	return result, preview, nil
}
