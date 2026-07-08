package model

import (
	"context"
	"strings"
	"time"

	keyutil "github.com/zyf2007/ChatAPI/internal/platform/apikey"
	"github.com/zyf2007/ChatAPI/internal/platform/secretbox"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Service) Authenticate(ctx context.Context, rawKey string) (Principal, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return Principal{}, common.ErrNotFound
	}
	item, err := s.store.GetModelAPIKeyByPrefix(ctx, keyutil.Prefix(rawKey))
	if err != nil || item.RevokedAt != nil {
		return Principal{}, common.ErrNotFound
	}
	storedRaw, err := secretbox.Open(item.KeyCiphertext, s.masterKey)
	if err != nil || storedRaw != rawKey {
		return Principal{}, common.ErrNotFound
	}
	now := time.Now().UTC()
	if item.LastUsedAt == nil || now.Sub(*item.LastUsedAt) >= lastUsedMinInterval {
		_ = s.store.UpdateModelAPIKeyLastUsedAt(ctx, item.ID, now)
	}
	return Principal{
		KeyID:     item.ID,
		UserID:    item.UserID,
		Name:      item.Name,
		KeyPrefix: item.KeyPrefix,
		Model:     item.Model,
	}, nil
}
