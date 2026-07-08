package admincontrol

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/repository/common"
)

func (s *Service) ListAppKeys(ctx context.Context, userID string) ([]common.AppAPIKey, error) {
	return s.keyStore.ListAppAPIKeysByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) RevokeAppKey(ctx context.Context, userID string, keyID string) error {
	return s.keyStore.RevokeAppAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}

func (s *Service) ListModelKeys(ctx context.Context, userID string) ([]common.ModelAPIKey, error) {
	return s.keyStore.ListModelAPIKeysByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) RevokeModelKey(ctx context.Context, userID string, keyID string) error {
	return s.keyStore.RevokeModelAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}
