package identity

import (
	"context"

	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/service/account"
	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

type Deps struct {
	Accounts *account.Service
	Logger   *zap.Logger
}

type Service struct {
	accounts *account.Service
	logger   *zap.Logger
}

func New(deps Deps) *Service {
	return &Service{accounts: deps.Accounts, logger: deps.Logger}
}

func (s *Service) ListLinkedIdentities(ctx context.Context, userID string) ([]store.UserIdentity, error) {
	items, err := s.accounts.ListUserIdentities(ctx, userID)
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", userID), zap.Int("identities.count", len(items))).Debug("usercontrol identity listed linked identities")
	}
	return items, err
}
