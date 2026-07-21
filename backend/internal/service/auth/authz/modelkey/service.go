package model

import (
	"strings"
	"time"

	"context"

	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"go.uber.org/zap"
)

type Service struct {
	store     auth.KeyStore
	masterKey string
	Logger    *zap.Logger
}

const lastUsedMinInterval = 5 * time.Minute

func NewService(dataStore auth.KeyStore, masterKey string) *Service {
	return &Service{store: dataStore, masterKey: strings.TrimSpace(masterKey)}
}

func (s *Service) ListKeysByUser(ctx context.Context, userID string) ([]common.ModelAPIKey, error) {
	return s.store.ListModelAPIKeysByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) ListModelsByUser(ctx context.Context, userID string) ([]common.VirtualModel, error) {
	return s.store.ListVirtualModelsByUser(ctx, strings.TrimSpace(userID))
}
