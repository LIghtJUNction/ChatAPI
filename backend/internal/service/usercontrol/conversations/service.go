package conversations

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/repository/common"
	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
	turnquerysvc "github.com/zyf/chatapi/internal/service/chat/turnquery"
	"go.uber.org/zap"
)

var ErrWaitingConversationDelete = errors.New("waiting conversation cannot be deleted")
var ErrForbidden = turnquerysvc.ErrForbidden

type queryService interface {
	ListConversationsForOwner(context.Context, string) ([]common.Conversation, error)
	ListMessagesForOwner(context.Context, string, string) ([]common.Message, error)
}

type turnService interface {
	ExecuteTurnControl(context.Context, turnsvc.TurnControlCommand) (map[string]any, error)
}

type Deps struct {
	Query      queryService
	Turn       turnService
	DeleteOne  func(context.Context, string) (common.DeleteConversationsResult, error)
	DeleteMany func(context.Context, []string) (common.DeleteConversationsResult, error)
	Logger     *zap.Logger
}

type Service struct {
	query      queryService
	turn       turnService
	deleteOne  func(context.Context, string) (common.DeleteConversationsResult, error)
	deleteMany func(context.Context, []string) (common.DeleteConversationsResult, error)
	logger     *zap.Logger
}

func New(deps Deps) *Service {
	return &Service{
		query:      deps.Query,
		turn:       deps.Turn,
		deleteOne:  deps.DeleteOne,
		deleteMany: deps.DeleteMany,
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

func (s *Service) AbortConversation(ctx context.Context, ownerID string, conversationID string, abortReason string) (map[string]any, error) {
	if _, err := s.query.ListMessagesForOwner(ctx, strings.TrimSpace(conversationID), strings.TrimSpace(ownerID)); err != nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(ownerID)), zap.String("conversation.id", strings.TrimSpace(conversationID))).Warn("usercontrol conversations abort rejected", zap.Error(err))
		return nil, err
	}
	result, err := s.turn.ExecuteTurnControl(ctx, turnsvc.TurnControlCommand{
		Kind:           turnsvc.TurnControlAbort,
		ConversationID: strings.TrimSpace(conversationID),
		AbortReason:    strings.TrimSpace(abortReason),
	})
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(ownerID)), zap.String("conversation.id", strings.TrimSpace(conversationID))).Info("usercontrol conversations aborted conversation")
	}
	return result, err
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
			if stringValue(item.Metadata["realtime_status"]) == "waiting" {
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
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(ownerID)), zap.String("conversation.id", strings.TrimSpace(conversationID))).Info("usercontrol conversations deleted conversation")
	}
	return result, err
}

func (s *Service) PruneConversations(ctx context.Context, ownerID string, keepCount int) (common.DeleteConversationsResult, int, error) {
	items, err := s.query.ListConversationsForOwner(ctx, strings.TrimSpace(ownerID))
	if err != nil {
		return common.DeleteConversationsResult{}, 0, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	deleteIDs := make([]string, 0)
	skipped := 0
	for idx, item := range items {
		if idx < keepCount {
			continue
		}
		if stringValue(item.Metadata["realtime_status"]) == "waiting" {
			skipped++
			continue
		}
		deleteIDs = append(deleteIDs, item.ID)
	}
	result, err := s.deleteMany(ctx, deleteIDs)
	if err != nil {
		return common.DeleteConversationsResult{}, 0, err
	}
	logging.BindContext(s.logger, ctx,
		zap.String("owner.id", strings.TrimSpace(ownerID)),
		zap.Int("keep_count", keepCount),
		zap.Int("deleted_count", result.DeletedConversations),
		zap.Int("skipped_count", skipped),
	).Info("usercontrol conversations pruned conversations")
	return result, skipped, nil
}

func stringValue(value any) string {
	raw, _ := value.(string)
	return strings.TrimSpace(raw)
}
