package chat

import (
	"context"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type Reader interface {
	ListConversations(context.Context) ([]common.Conversation, error)
	ListConversationsForOwner(context.Context, string) ([]common.Conversation, error)
	CountConversationsForOwner(context.Context, string) (int, error)
	ListConversationsForOwnerPage(context.Context, string, time.Time, string, int) ([]common.Conversation, error)
	GetConversation(context.Context, string) (common.Conversation, error)
	FindConversationByToolCallID(context.Context, string, string) (common.Conversation, error)
	ListRequests(context.Context) ([]common.Request, error)
	GetRequest(context.Context, string) (common.Request, error)
	GetLatestRequestForConversation(context.Context, string) (common.Request, error)
	ListMessages(context.Context, string) ([]common.Message, error)
	ListConversationEvents(context.Context, string) ([]common.ConversationEvent, error)
	ListMediaAssets(context.Context) ([]common.MediaAsset, error)
	CreateMediaAsset(context.Context, common.CreateMediaAssetInput) (common.MediaAsset, error)
	GetMediaAssetByID(context.Context, string) (common.MediaAsset, error)
	GetStagedMediaAsset(context.Context, string, string, string, string) (common.MediaAsset, error)
	GetMediaAssetByFileID(context.Context, string) (common.MediaAsset, error)
	ListOrphanMediaAssets(context.Context) ([]common.MediaAsset, error)
}

type Writer interface {
	DeleteConversations(context.Context, []string) (common.DeleteConversationsResult, error)
	DeleteMediaAssetsByIDs(context.Context, []string) (int, error)
	ExpirePendingTurns(context.Context, time.Time) (common.ExpirePendingTurnsResult, error)
	CreatePendingTurn(context.Context, common.CreatePendingInput) (common.Conversation, common.Message, error)
	UpdateDraft(context.Context, common.UpdateDraftInput) (common.Conversation, error)
	CompletePendingTurn(context.Context, common.CompletePendingInput) (common.Conversation, common.Message, error)
	AbortPendingTurn(context.Context, common.AbortPendingInput) (common.Conversation, common.Message, error)
	DisconnectPendingTurn(context.Context, common.DisconnectPendingInput) (common.Conversation, common.Message, error)
	AbortPendingTurnWithEvent(context.Context, common.PendingTurnLifecycleMutationInput) (common.PendingTurnMutationResult, error)
	DisconnectPendingTurnWithEvent(context.Context, common.PendingTurnLifecycleMutationInput) (common.PendingTurnMutationResult, error)
	DisconnectAllPendingTurns(context.Context, string) (common.ExpirePendingTurnsResult, error)
	AppendConversationEvent(context.Context, common.AppendConversationEventInput) (common.ConversationEvent, error)
	AppendConversationEventWithAsset(context.Context, common.AppendConversationEventWithAssetInput) (common.ConversationEvent, error)
}

type Store interface {
	Reader
	Writer
}
