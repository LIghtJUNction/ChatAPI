package postgresql

import (
	"context"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

func (s *Store) MigrationStatus(context.Context) (store.MigrationStatus, error) {
	return store.MigrationStatus{}, errNotImplemented
}

func (s *Store) Checkpoint(context.Context) error { return nil }

func (s *Store) Vacuum(context.Context) error { return nil }

func (s *Store) ListConversations(context.Context) ([]store.Conversation, error) {
	return nil, errNotImplemented
}

func (s *Store) GetConversation(context.Context, string) (store.Conversation, error) {
	return store.Conversation{}, errNotImplemented
}

func (s *Store) ListRequests(context.Context) ([]store.Request, error) {
	return nil, errNotImplemented
}

func (s *Store) GetRequest(context.Context, string) (store.Request, error) {
	return store.Request{}, errNotImplemented
}

func (s *Store) ListAutomationRulesByUser(context.Context, string) ([]store.AutomationRule, error) {
	return nil, errNotImplemented
}

func (s *Store) ReplaceAutomationRulesForUser(context.Context, string, map[string]struct{}, []store.UpsertAutomationRuleInput) ([]store.AutomationRule, error) {
	return nil, errNotImplemented
}

func (s *Store) CreateUploadedImage(context.Context, store.CreateUploadedImageInput) (store.UploadedImage, error) {
	return store.UploadedImage{}, errNotImplemented
}

func (s *Store) ListUploadedImages(context.Context) ([]store.UploadedImage, error) {
	return nil, errNotImplemented
}

func (s *Store) ListUploadedImagesByOwner(context.Context, string) ([]store.UploadedImage, error) {
	return nil, errNotImplemented
}

func (s *Store) DeleteUploadedImagesByFilenames(context.Context, []string) (store.DeleteUploadedImagesResult, error) {
	return store.DeleteUploadedImagesResult{}, errNotImplemented
}

func (s *Store) UpsertStorageFileDeletionFailure(context.Context, store.UpsertStorageFileDeletionFailureInput) (store.StorageFileDeletionFailure, error) {
	return store.StorageFileDeletionFailure{}, errNotImplemented
}

func (s *Store) ListStorageFileDeletionFailures(context.Context, int) ([]store.StorageFileDeletionFailure, error) {
	return nil, errNotImplemented
}

func (s *Store) DeleteStorageFileDeletionFailures(context.Context, []string) error {
	return errNotImplemented
}

func (s *Store) ListStorageUserQuotas(context.Context) ([]store.StorageUserQuota, error) {
	return nil, errNotImplemented
}

func (s *Store) GetStorageUserQuota(context.Context, string) (store.StorageUserQuota, error) {
	return store.StorageUserQuota{}, errNotImplemented
}

func (s *Store) SetStorageUserQuota(context.Context, string, int64) (store.StorageUserQuota, error) {
	return store.StorageUserQuota{}, errNotImplemented
}

func (s *Store) DeleteStorageUserQuota(context.Context, string) error {
	return errNotImplemented
}

func (s *Store) ListMessages(context.Context, string) ([]store.Message, error) {
	return nil, errNotImplemented
}

func (s *Store) DeleteConversations(context.Context, []string) (store.DeleteConversationsResult, error) {
	return store.DeleteConversationsResult{}, errNotImplemented
}

func (s *Store) ExpirePendingTurns(context.Context, time.Time) (store.ExpirePendingTurnsResult, error) {
	return store.ExpirePendingTurnsResult{}, errNotImplemented
}

func (s *Store) CreatePendingTurn(context.Context, store.CreatePendingInput) (store.Conversation, store.Message, error) {
	return store.Conversation{}, store.Message{}, errNotImplemented
}

func (s *Store) UpdateDraft(context.Context, store.UpdateDraftInput) (store.Conversation, error) {
	return store.Conversation{}, errNotImplemented
}

func (s *Store) CompletePendingTurn(context.Context, store.CompletePendingInput) (store.Conversation, store.Message, error) {
	return store.Conversation{}, store.Message{}, errNotImplemented
}

func (s *Store) AbortPendingTurn(context.Context, store.AbortPendingInput) (store.Conversation, store.Message, error) {
	return store.Conversation{}, store.Message{}, errNotImplemented
}
