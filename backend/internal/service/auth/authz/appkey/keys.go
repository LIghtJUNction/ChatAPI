package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	keyutil "github.com/zyf2007/ChatAPI/internal/platform/apikey"
	"github.com/zyf2007/ChatAPI/internal/platform/secretbox"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Service) CreateKey(ctx context.Context, userID string, name string, scopes []string, resourceLimits map[string]any, expiresAt *time.Time) (common.AppAPIKey, string, error) {
	raw := "ak-" + uuid.NewString()
	ciphertext, err := secretbox.Seal(raw, s.masterKey)
	if err != nil {
		return common.AppAPIKey{}, "", err
	}
	item, err := s.store.CreateAppAPIKey(ctx, common.CreateAppAPIKeyInput{
		ID:             "appkey_" + uuid.NewString(),
		UserID:         strings.TrimSpace(userID),
		Name:           strings.TrimSpace(name),
		KeyHash:        keyutil.Hash(raw),
		KeyCiphertext:  ciphertext,
		KeyPrefix:      keyutil.Prefix(raw),
		Scopes:         append([]string(nil), scopes...),
		ResourceLimits: cloneMap(resourceLimits),
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		return common.AppAPIKey{}, "", err
	}
	return item, raw, nil
}

func (s *Service) RevealKey(ctx context.Context, userID, keyID string) (string, error) {
	item, err := s.store.GetAppAPIKeyByID(ctx, strings.TrimSpace(keyID))
	if err != nil || item.UserID != strings.TrimSpace(userID) {
		return "", common.ErrNotFound
	}
	return secretbox.Open(item.KeyCiphertext, s.masterKey)
}
