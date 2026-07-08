package admin

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/service/account"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	"github.com/zyf/chatapi/internal/store"
)

type Service struct {
	accounts *account.Service
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

func NewService(accounts *account.Service, dataStore store.Store, policies *policy.Service) *Service {
	return &Service{accounts: accounts, store: dataStore, policies: policies}
}

func (s *Service) ListUsers(ctx context.Context) ([]store.User, error) {
	return s.accounts.ListUsers(ctx)
}

func (s *Service) GetUser(ctx context.Context, userID string) (store.User, error) {
	return s.accounts.GetUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) CreateUser(ctx context.Context, input CreateUserInput) (store.User, error) {
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "user"
	}
	return s.accounts.CreateUser(ctx, account.CreateUserInput{
		Username:   strings.TrimSpace(input.Username),
		Email:      strings.ToLower(strings.TrimSpace(input.Email)),
		Password:   strings.TrimSpace(input.Password),
		Role:       role,
		IsActive:   input.IsActive,
		LocalAdmin: input.LocalAdmin,
	})
}

func (s *Service) SetUserState(ctx context.Context, userID string, isActive bool) (store.User, error) {
	return s.accounts.SetUserState(ctx, userID, isActive)
}

func (s *Service) ResetPassword(ctx context.Context, userID string, newPassword string) (store.User, error) {
	return s.accounts.SetPassword(ctx, userID, newPassword)
}

func (s *Service) ListUserIdentities(ctx context.Context, userID string) ([]store.UserIdentity, error) {
	return s.accounts.ListUserIdentities(ctx, strings.TrimSpace(userID))
}

func (s *Service) DeleteUserIdentity(ctx context.Context, userID string, identityID string) error {
	return s.accounts.DeleteUserIdentity(ctx, strings.TrimSpace(identityID), strings.TrimSpace(userID))
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
	return s.accounts.PreviewDeletion(ctx, strings.TrimSpace(userID))
}

func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	return s.accounts.DeleteUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) TransferOwnership(ctx context.Context, sourceUserID string, targetUserID string) (store.UserOwnershipTransferResult, error) {
	return s.accounts.TransferOwnership(ctx, strings.TrimSpace(sourceUserID), strings.TrimSpace(targetUserID))
}

func (s *Service) TransferOwnershipSelection(ctx context.Context, sourceUserID string, targetUserID string, conversationIDs []string, filenames []string) (store.UserOwnershipTransferResult, error) {
	return s.accounts.TransferOwnershipSelection(ctx, strings.TrimSpace(sourceUserID), strings.TrimSpace(targetUserID), conversationIDs, filenames)
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
