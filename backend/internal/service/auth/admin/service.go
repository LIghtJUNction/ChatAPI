package admin

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/zyf/chatapi/internal/platform/password"
	"github.com/zyf/chatapi/internal/service/auth/policy"
	"github.com/zyf/chatapi/internal/store"
)

type Service struct {
	store    store.Store
	policies *policy.Service
}

type CreateUserInput struct {
	Username   string
	Email      string
	Password   string
	Role       string
	IsActive   bool
	LocalAdmin bool
}

func NewService(dataStore store.Store, policies *policy.Service) *Service {
	return &Service{store: dataStore, policies: policies}
}

func (s *Service) ListUsers(ctx context.Context) ([]store.User, error) {
	return s.store.ListUsers(ctx)
}

func (s *Service) GetUser(ctx context.Context, userID string) (store.User, error) {
	return s.store.GetUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) CreateUser(ctx context.Context, input CreateUserInput) (store.User, error) {
	hashed := ""
	if strings.TrimSpace(input.Password) != "" {
		value, err := password.Hash(strings.TrimSpace(input.Password))
		if err != nil {
			return store.User{}, err
		}
		hashed = value
	}
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "user"
	}
	return s.store.CreateUser(ctx, store.CreateUserInput{
		ID:           "user_" + uuid.NewString(),
		Username:     strings.TrimSpace(input.Username),
		Email:        strings.ToLower(strings.TrimSpace(input.Email)),
		PasswordHash: hashed,
		Role:         role,
		IsActive:     input.IsActive,
		LocalAdmin:   input.LocalAdmin,
	})
}

func (s *Service) SetUserState(ctx context.Context, userID string, isActive bool) (store.User, error) {
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return store.User{}, err
	}
	return s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		Role:         user.Role,
		IsActive:     isActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  user.LastLoginAt,
	})
}

func (s *Service) ResetPassword(ctx context.Context, userID string, newPassword string) (store.User, error) {
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return store.User{}, err
	}
	hashed, err := password.Hash(strings.TrimSpace(newPassword))
	if err != nil {
		return store.User{}, err
	}
	return s.store.UpdateUser(ctx, store.UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: hashed,
		Role:         user.Role,
		IsActive:     user.IsActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  user.LastLoginAt,
	})
}

func (s *Service) ListUserIdentities(ctx context.Context, userID string) ([]store.UserIdentity, error) {
	return s.store.ListUserIdentities(ctx, strings.TrimSpace(userID))
}

func (s *Service) DeleteUserIdentity(ctx context.Context, userID string, identityID string) error {
	return s.store.DeleteUserIdentity(ctx, strings.TrimSpace(identityID), strings.TrimSpace(userID))
}

func (s *Service) ListAppKeys(ctx context.Context, userID string) ([]store.AppAPIKey, error) {
	return s.store.ListAppAPIKeysByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) RevokeAppKey(ctx context.Context, userID string, keyID string) error {
	return s.store.RevokeAppAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}

func (s *Service) ListModelKeys(ctx context.Context, userID string) ([]store.ModelAPIKey, error) {
	return s.store.ListModelAPIKeysByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) RevokeModelKey(ctx context.Context, userID string, keyID string) error {
	return s.store.RevokeModelAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}

func (s *Service) DeletePreview(ctx context.Context, userID string) (store.UserDeletionPreview, error) {
	return s.store.PreviewUserDeletion(ctx, strings.TrimSpace(userID))
}

func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	return s.store.DeleteUserAccount(ctx, strings.TrimSpace(userID))
}

func (s *Service) TransferOwnership(ctx context.Context, sourceUserID string, targetUserID string) (store.UserOwnershipTransferResult, error) {
	return s.store.TransferUserOwnership(ctx, strings.TrimSpace(sourceUserID), strings.TrimSpace(targetUserID))
}

func (s *Service) TransferOwnershipSelection(ctx context.Context, sourceUserID string, targetUserID string, conversationIDs []string, filenames []string) (store.UserOwnershipTransferResult, error) {
	return s.store.TransferUserOwnershipSelection(ctx, strings.TrimSpace(sourceUserID), strings.TrimSpace(targetUserID), conversationIDs, filenames)
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
