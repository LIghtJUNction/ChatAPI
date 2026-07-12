package keys

import (
	"context"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	appkeysvc "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkeysvc "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"go.uber.org/zap"
)

type Deps struct {
	Keys      auth.KeyStore
	AppKeys   *appkeysvc.Service
	ModelKeys *modelkeysvc.Service
	Logger    *zap.Logger
}

type Service struct {
	store     auth.KeyStore
	appKeys   *appkeysvc.Service
	modelKeys *modelkeysvc.Service
	logger    *zap.Logger
}

func New(deps Deps) *Service {
	return &Service{store: deps.Keys, appKeys: deps.AppKeys, modelKeys: deps.ModelKeys, logger: deps.Logger}
}

func (s *Service) ListAppKeys(ctx context.Context, userID string) ([]common.AppAPIKey, error) {
	items, err := s.store.ListAppAPIKeysByUser(ctx, strings.TrimSpace(userID))
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.Int("app_keys.count", len(items))).Debug("usercontrol keys listed app keys")
	}
	return items, err
}

func (s *Service) CreateAppKey(ctx context.Context, userID string, name string, scopes []string, resourceLimits map[string]any, expiresAt *time.Time) (common.AppAPIKey, string, error) {
	item, raw, err := s.appKeys.CreateKey(ctx, strings.TrimSpace(userID), strings.TrimSpace(name), scopes, resourceLimits, expiresAt)
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("app_key.id", item.ID)).Info("usercontrol keys created app key")
	}
	return item, raw, err
}

func (s *Service) RevokeAppKey(ctx context.Context, userID string, keyID string) error {
	err := s.store.RevokeAppAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("app_key.id", strings.TrimSpace(keyID))).Info("usercontrol keys revoked app key")
	}
	return err
}

func (s *Service) ListModelKeys(ctx context.Context, userID string) ([]common.ModelAPIKey, error) {
	items, err := s.store.ListModelAPIKeysByUser(ctx, strings.TrimSpace(userID))
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.Int("model_keys.count", len(items))).Debug("usercontrol keys listed model keys")
	}
	return items, err
}

func (s *Service) CreateModelKey(ctx context.Context, userID string, name string, model string) (common.ModelAPIKey, string, error) {
	item, raw, err := s.modelKeys.CreateKey(ctx, strings.TrimSpace(userID), strings.TrimSpace(name), strings.TrimSpace(model))
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("model_key.id", item.ID), zap.String("model", item.Model)).Info("usercontrol keys created model key")
	}
	return item, raw, err
}

func (s *Service) RevokeModelKey(ctx context.Context, userID string, keyID string) error {
	err := s.store.RevokeModelAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("model_key.id", strings.TrimSpace(keyID))).Info("usercontrol keys revoked model key")
	}
	return err
}
