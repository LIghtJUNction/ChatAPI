package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	keyutil "github.com/zyf/chatapi/internal/platform/apikey"
	"github.com/zyf/chatapi/internal/store"
)

func (s *Service) CreateKey(ctx context.Context, userID string, name string, scopes []string, resourceLimits map[string]any, expiresAt *time.Time) (store.AppAPIKey, string, error) {
	raw := "ak-" + uuid.NewString()
	item, err := s.store.CreateAppAPIKey(ctx, store.CreateAppAPIKeyInput{
		ID:             "appkey_" + uuid.NewString(),
		UserID:         strings.TrimSpace(userID),
		Name:           strings.TrimSpace(name),
		KeyHash:        keyutil.Hash(raw),
		KeyPrefix:      keyutil.Prefix(raw),
		Scopes:         append([]string(nil), scopes...),
		ResourceLimits: cloneMap(resourceLimits),
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		return store.AppAPIKey{}, "", err
	}
	return item, raw, nil
}
