package conversations

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	conversationstate "github.com/zyf2007/ChatAPI/internal/service/chat/conversationstate"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	"go.uber.org/zap"
)

var ErrWaitingConversationDelete = errors.New("waiting conversation cannot be deleted")
var ErrForbidden = turnquerysvc.ErrForbidden

type queryService interface {
	ListConversationsForOwner(context.Context, string) ([]common.Conversation, error)
	ListMessagesForOwner(context.Context, string, string) ([]common.Message, error)
}

type turnService interface {
	Execute(context.Context, controlsvc.Command) (controlsvc.Result, error)
}

type Deps struct {
	Query      queryService
	Turn       turnService
	DeleteOne  func(context.Context, string) (common.DeleteConversationsResult, error)
	DeleteMany func(context.Context, []string) (common.DeleteConversationsResult, error)
	Events     chatevents.Publisher
	Logger     *zap.Logger
}

type Service struct {
	query      queryService
	turn       turnService
	deleteOne  func(context.Context, string) (common.DeleteConversationsResult, error)
	deleteMany func(context.Context, []string) (common.DeleteConversationsResult, error)
	events     chatevents.Publisher
	logger     *zap.Logger
}

func New(deps Deps) *Service {
	return &Service{
		query:      deps.Query,
		turn:       deps.Turn,
		deleteOne:  deps.DeleteOne,
		deleteMany: deps.DeleteMany,
		events:     deps.Events,
		logger:     deps.Logger,
	}
}

func (s *Service) ListConversations(ctx context.Context, ownerID string) ([]common.Conversation, error) {
	items, err := s.query.ListConversationsForOwner(ctx, strings.TrimSpace(ownerID))
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(ownerID)), zap.Int("conversations.count", len(items))).Debug("usercontrol conversations listed conversations")
	}
	return items, err
}

func (s *Service) ListConversationMessages(ctx context.Context, ownerID string, conversationID string) ([]common.Message, error) {
	items, err := s.query.ListMessagesForOwner(ctx, strings.TrimSpace(conversationID), strings.TrimSpace(ownerID))
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(ownerID)), zap.String("conversation.id", strings.TrimSpace(conversationID)), zap.Int("messages.count", len(items))).Debug("usercontrol conversations listed messages")
	}
	return items, err
}

func (s *Service) AbortConversation(ctx context.Context, ownerID string, conversationID string, requestID string, abortReason string) (map[string]any, error) {
	result, err := s.turn.Execute(ctx, controlsvc.Command{
		Source:         controlsvc.SourceAPI,
		OwnerID:        strings.TrimSpace(ownerID),
		ConversationID: strings.TrimSpace(conversationID),
		RequestID:      strings.TrimSpace(requestID),
		Action: turnsvc.OutputAction{
			Kind:        turnsvc.TurnControlAbort,
			AbortReason: strings.TrimSpace(abortReason),
		},
	})
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(ownerID)), zap.String("conversation.id", strings.TrimSpace(conversationID))).Info("usercontrol conversations aborted conversation")
	}
	return result.Body, err
}

func (s *Service) DeleteConversation(ctx context.Context, ownerID string, conversationID string) (common.DeleteConversationsResult, error) {
	conversations, err := s.query.ListConversationsForOwner(ctx, strings.TrimSpace(ownerID))
	if err != nil {
		return common.DeleteConversationsResult{}, err
	}
	found := false
	for _, item := range conversations {
		if item.ID == strings.TrimSpace(conversationID) {
			found = true
			if conversationstate.FromConversation(item).Status == conversationstate.StatusWaiting {
				logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(ownerID)), zap.String("conversation.id", strings.TrimSpace(conversationID))).Warn("usercontrol conversations delete rejected waiting conversation")
				return common.DeleteConversationsResult{}, ErrWaitingConversationDelete
			}
			break
		}
	}
	if !found {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(ownerID)), zap.String("conversation.id", strings.TrimSpace(conversationID))).Warn("usercontrol conversations delete rejected missing conversation")
		return common.DeleteConversationsResult{}, common.ErrNotFound
	}
	result, err := s.deleteOne(ctx, strings.TrimSpace(conversationID))
	if err == nil {
		chatevents.PublishDeletedConversations(ctx, s.events, result)
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(ownerID)), zap.String("conversation.id", strings.TrimSpace(conversationID))).Info("usercontrol conversations deleted conversation")
	}
	return result, err
}

func (s *Service) PruneConversations(ctx context.Context, ownerID string, keepCount int) (common.DeleteConversationsResult, int, error) {
	items, err := s.query.ListConversationsForOwner(ctx, strings.TrimSpace(ownerID))
	if err != nil {
		return common.DeleteConversationsResult{}, 0, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	deleteIDs := make([]string, 0)
	skipped := 0
	for idx, item := range items {
		if idx < keepCount {
			continue
		}
		if conversationstate.FromConversation(item).Status == conversationstate.StatusWaiting {
			skipped++
			continue
		}
		deleteIDs = append(deleteIDs, item.ID)
	}
	result, err := s.deleteMany(ctx, deleteIDs)
	if err != nil {
		return common.DeleteConversationsResult{}, 0, err
	}
	chatevents.PublishDeletedConversations(ctx, s.events, result)
	logging.BindContext(s.logger, ctx,
		zap.String("owner.id", strings.TrimSpace(ownerID)),
		zap.Int("keep_count", keepCount),
		zap.Int("deleted_count", result.DeletedConversations),
		zap.Int("skipped_count", skipped),
	).Info("usercontrol conversations pruned conversations")
	return result, skipped, nil
}
