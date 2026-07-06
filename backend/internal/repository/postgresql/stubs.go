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
