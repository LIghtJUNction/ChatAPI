package admin

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

func (s *Service) ListAppKeys(ctx context.Context, userID string) ([]store.AppAPIKey, error) {
	return s.store.ListAppAPIKeysByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) RevokeAppKey(ctx context.Context, userID string, keyID string) error {
	return s.store.RevokeAppAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}

func (s *Service) ListModelKeys(ctx context.Context, userID string) ([]store.ModelAPIKey, error) {
	return s.store.ListModelAPIKeysByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) RevokeModelKey(ctx context.Context, userID string, keyID string) error {
	return s.store.RevokeModelAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}
