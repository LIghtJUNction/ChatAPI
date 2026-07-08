package chatrepo

import (
	"context"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

type Reader interface {
	ListConversations(context.Context) ([]store.Conversation, error)
	GetConversation(context.Context, string) (store.Conversation, error)
	ListRequests(context.Context) ([]store.Request, error)
	GetRequest(context.Context, string) (store.Request, error)
	ListMessages(context.Context, string) ([]store.Message, error)
	ListMediaAssets(context.Context) ([]store.MediaAsset, error)
	ListOrphanMediaAssets(context.Context) ([]store.MediaAsset, error)
}

type Writer interface {
	DeleteConversations(context.Context, []string) (store.DeleteConversationsResult, error)
	DeleteMediaAssetsByIDs(context.Context, []string) (int, error)
	ExpirePendingTurns(context.Context, time.Time) (store.ExpirePendingTurnsResult, error)
	CreatePendingTurn(context.Context, store.CreatePendingInput) (store.Conversation, store.Message, error)
	UpdateDraft(context.Context, store.UpdateDraftInput) (store.Conversation, error)
	CompletePendingTurn(context.Context, store.CompletePendingInput) (store.Conversation, store.Message, error)
	AbortPendingTurn(context.Context, store.AbortPendingInput) (store.Conversation, store.Message, error)
}

type Store interface {
	Reader
	Writer
}
