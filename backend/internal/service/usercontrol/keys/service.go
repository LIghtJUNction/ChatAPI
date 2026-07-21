package keys

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

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
	if err != nil {
		return nil, err
	}
	active := make([]common.AppAPIKey, 0, len(items))
	for _, item := range items {
		if item.RevokedAt == nil {
			active = append(active, item)
		}
	}
	logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.Int("app_keys.count", len(active))).Debug("usercontrol keys listed app keys")
	return active, nil
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

func (s *Service) RevealAppKey(ctx context.Context, userID, keyID string) (string, error) {
	return s.appKeys.RevealKey(ctx, userID, keyID)
}

func (s *Service) ListModelKeys(ctx context.Context, userID string) ([]common.ModelAPIKey, error) {
	items, err := s.store.ListModelAPIKeysByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	active := make([]common.ModelAPIKey, 0, len(items))
	for _, item := range items {
		if item.RevokedAt == nil {
			active = append(active, item)
		}
	}
	logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.Int("model_keys.count", len(active))).Debug("usercontrol keys listed model keys")
	return active, nil
}

func (s *Service) CreateModelKey(ctx context.Context, userID string, name string, rawKey string) (common.ModelAPIKey, string, error) {
	item, raw, err := s.modelKeys.CreateKey(ctx, strings.TrimSpace(userID), strings.TrimSpace(name), strings.TrimSpace(rawKey))
	if err == nil {
		item.Model = strings.TrimSpace(rawKey)
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("model_key.id", item.ID)).Info("usercontrol keys created model key")
	}
	return item, raw, err
}

func (s *Service) CreateManualModelKey(ctx context.Context, userID string, name string, rawKey string) (common.ModelAPIKey, string, error) {
	return s.modelKeys.CreateKeyWithRaw(ctx, strings.TrimSpace(userID), strings.TrimSpace(name), strings.TrimSpace(rawKey))
}

func (s *Service) ListVirtualModels(ctx context.Context, userID string) ([]common.VirtualModel, error) {
	return s.store.ListVirtualModelsByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) CreateVirtualModel(ctx context.Context, userID, name string) (common.VirtualModel, error) {
	return s.store.CreateVirtualModel(ctx, common.CreateVirtualModelInput{ID: "vmodel_" + uuid.NewString(), UserID: strings.TrimSpace(userID), Name: strings.TrimSpace(name)})
}

func (s *Service) DeleteVirtualModel(ctx context.Context, userID, modelID string) error {
	return s.store.DeleteVirtualModel(ctx, strings.TrimSpace(modelID), strings.TrimSpace(userID))
}

func (s *Service) RevokeModelKey(ctx context.Context, userID string, keyID string) error {
	err := s.store.RevokeModelAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("model_key.id", strings.TrimSpace(keyID))).Info("usercontrol keys revoked model key")
	}
	return err
}

func (s *Service) RevealModelKey(ctx context.Context, userID, keyID string) (string, error) {
	return s.modelKeys.RevealKey(ctx, userID, keyID)
}
