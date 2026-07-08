package model

import (
	"context"
	"strings"

	"github.com/google/uuid"

	keyutil "github.com/zyf2007/ChatAPI/internal/platform/apikey"
	"github.com/zyf2007/ChatAPI/internal/platform/secretbox"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Service) CreateKey(ctx context.Context, userID string, name string, modelName string) (common.ModelAPIKey, string, error) {
	raw := "sk-" + uuid.NewString()
	ciphertext, err := secretbox.Seal(raw, s.masterKey)
	if err != nil {
		return common.ModelAPIKey{}, "", err
	}
	item, err := s.store.CreateModelAPIKey(ctx, common.CreateModelAPIKeyInput{
		ID:            "modelkey_" + uuid.NewString(),
		UserID:        strings.TrimSpace(userID),
		Name:          strings.TrimSpace(name),
		KeyCiphertext: ciphertext,
		KeyPrefix:     keyutil.Prefix(raw),
		Model:         strings.TrimSpace(modelName),
	})
	if err != nil {
		return common.ModelAPIKey{}, "", err
	}
	item.RawKey = raw
	return item, raw, nil
}
