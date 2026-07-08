package model

import (
	"context"
	"strings"

	"github.com/google/uuid"

	keyutil "github.com/zyf/chatapi/internal/platform/apikey"
	"github.com/zyf/chatapi/internal/platform/secretbox"
	"github.com/zyf/chatapi/internal/store"
)

func (s *Service) CreateKey(ctx context.Context, userID string, name string, modelName string) (store.ModelAPIKey, string, error) {
	raw := "sk-" + uuid.NewString()
	ciphertext, err := secretbox.Seal(raw, s.masterKey)
	if err != nil {
		return store.ModelAPIKey{}, "", err
	}
	item, err := s.store.CreateModelAPIKey(ctx, store.CreateModelAPIKeyInput{
		ID:            "modelkey_" + uuid.NewString(),
		UserID:        strings.TrimSpace(userID),
		Name:          strings.TrimSpace(name),
		KeyCiphertext: ciphertext,
		KeyPrefix:     keyutil.Prefix(raw),
		Model:         strings.TrimSpace(modelName),
	})
	if err != nil {
		return store.ModelAPIKey{}, "", err
	}
	item.RawKey = raw
	return item, raw, nil
}
