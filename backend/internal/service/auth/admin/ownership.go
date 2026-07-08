package admin

import (
	"context"

	"github.com/zyf/chatapi/internal/store"
)

func (s *Service) TransferOwnership(ctx context.Context, sourceUserID string, targetUserID string) (store.UserOwnershipTransferResult, error) {
	return s.accounts.TransferOwnership(ctx, sourceUserID, targetUserID)
}

func (s *Service) TransferOwnershipSelection(ctx context.Context, sourceUserID string, targetUserID string, conversationIDs []string, filenames []string) (store.UserOwnershipTransferResult, error) {
	return s.accounts.TransferOwnershipSelection(ctx, sourceUserID, targetUserID, conversationIDs, filenames)
}

func (s *Service) OwnershipItems(ctx context.Context, userID string) (store.UserOwnershipSelection, error) {
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return store.UserOwnershipSelection{}, err
	}
	conversations, err := s.store.ListConversations(ctx)
	if err != nil {
		return store.UserOwnershipSelection{}, err
	}
	filteredConversations := make([]store.UserOwnedConversationItem, 0)
	for _, item := range conversations {
		if stringValue(item.Metadata["owner_id"], "") == user.ID {
			filteredConversations = append(filteredConversations, store.UserOwnedConversationItem{
				ConversationID: item.ID,
				Title:          item.Title,
				LastMessageAt:  item.LastMessageAt,
				MessageCount:   item.MessageCount,
			})
		}
	}
	uploads, err := s.store.ListUploadedImagesByOwner(ctx, user.ID)
	if err != nil {
		return store.UserOwnershipSelection{}, err
	}
	filteredUploads := make([]store.UserOwnedUploadItem, 0, len(uploads))
	for _, item := range uploads {
		filteredUploads = append(filteredUploads, store.UserOwnedUploadItem{
			ID:        item.ID,
			Filename:  item.Filename,
			Bytes:     item.Bytes,
			CreatedAt: item.CreatedAt,
		})
	}
	return store.UserOwnershipSelection{User: user, Conversations: filteredConversations, Uploads: filteredUploads}, nil
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && raw != "" {
		return raw
	}
	return fallback
}
