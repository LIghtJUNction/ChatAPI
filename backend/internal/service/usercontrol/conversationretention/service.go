package conversationretention

import (
	"context"
	"slices"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"go.uber.org/zap"
)

const LimitSettingKey = "user_conversation_limit"

type UserLister interface {
	ListUsers(context.Context) ([]common.User, error)
}

type Pruner interface {
	PruneConversations(context.Context, string, int) (common.DeleteConversationsResult, int, error)
}

type Service struct {
	users  UserLister
	pruner Pruner
	limit  func(context.Context) int
	logger *zap.Logger
}

func New(users UserLister, pruner Pruner, limit func(context.Context) int, logger *zap.Logger) *Service {
	return &Service{users: users, pruner: pruner, limit: limit, logger: logger}
}

func (s *Service) Enforce(ctx context.Context, ownerID string) {
	if s == nil || s.pruner == nil || s.limit == nil {
		return
	}
	limit := s.limit(ctx)
	if limit <= 0 {
		return
	}
	if _, _, err := s.pruner.PruneConversations(ctx, ownerID, limit); err != nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", ownerID), zap.Int("conversation.limit", limit)).Warn("failed to enforce conversation retention", zap.Error(err))
	}
}

func (s *Service) SettingsUpdated(ctx context.Context, changedKeys []string) {
	if s == nil || !slices.Contains(changedKeys, LimitSettingKey) || s.users == nil {
		return
	}
	users, err := s.users.ListUsers(ctx)
	if err != nil {
		logging.BindContext(s.logger, ctx).Warn("failed to list users for conversation retention reconciliation", zap.Error(err))
		return
	}
	for _, user := range users {
		s.Enforce(ctx, user.ID)
	}
}
