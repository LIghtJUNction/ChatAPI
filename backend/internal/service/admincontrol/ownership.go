package admincontrol

import (
	"context"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	conversationstate "github.com/zyf2007/ChatAPI/internal/service/chat/conversationstate"
)

func (s *Service) TransferOwnership(ctx context.Context, sourceUserID string, targetUserID string) (common.UserOwnershipTransferResult, error) {
	return s.accounts.TransferOwnership(ctx, sourceUserID, targetUserID)
}

func (s *Service) TransferOwnershipSelection(ctx context.Context, sourceUserID string, targetUserID string, conversationIDs []string, filenames []string) (common.UserOwnershipTransferResult, error) {
	return s.accounts.TransferOwnershipSelection(ctx, sourceUserID, targetUserID, conversationIDs, filenames)
}

func (s *Service) OwnershipItems(ctx context.Context, userID string) (common.UserOwnershipSelection, error) {
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return common.UserOwnershipSelection{}, err
	}
	conversations, err := s.chatStore.ListConversations(ctx)
	if err != nil {
		return common.UserOwnershipSelection{}, err
	}
	filteredConversations := make([]common.UserOwnedConversationItem, 0)
	for _, item := range conversations {
		if conversationstate.OwnerID(item) == user.ID {
			filteredConversations = append(filteredConversations, common.UserOwnedConversationItem{
				ConversationID: item.ID,
				Title:          item.Title,
				LastMessageAt:  item.LastMessageAt,
				MessageCount:   item.MessageCount,
			})
		}
	}
	uploads, err := s.storageStore.ListUploadedImagesByOwner(ctx, user.ID)
	if err != nil {
		return common.UserOwnershipSelection{}, err
	}
	filteredUploads := make([]common.UserOwnedUploadItem, 0, len(uploads))
	for _, item := range uploads {
		filteredUploads = append(filteredUploads, common.UserOwnedUploadItem{
			ID:        item.ID,
			Filename:  item.Filename,
			Bytes:     item.Bytes,
			CreatedAt: item.CreatedAt,
		})
	}
	return common.UserOwnershipSelection{User: user, Conversations: filteredConversations, Uploads: filteredUploads}, nil
}
