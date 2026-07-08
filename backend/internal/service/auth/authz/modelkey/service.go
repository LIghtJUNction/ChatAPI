package model

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	keyutil "github.com/zyf/chatapi/internal/platform/apikey"
	"github.com/zyf/chatapi/internal/platform/secretbox"
	"github.com/zyf/chatapi/internal/store"
)

type Principal struct {
	KeyID     string
	UserID    string
	Name      string
	KeyPrefix string
	Model     string
}

type Service struct {
	store     store.Store
	masterKey string
}

const lastUsedMinInterval = 5 * time.Minute

func NewService(dataStore store.Store, masterKey string) *Service {
	return &Service{store: dataStore, masterKey: strings.TrimSpace(masterKey)}
}

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

func (s *Service) Authenticate(ctx context.Context, rawKey string) (Principal, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return Principal{}, store.ErrNotFound
	}
	item, err := s.store.GetModelAPIKeyByPrefix(ctx, keyutil.Prefix(rawKey))
	if err != nil || item.RevokedAt != nil {
		return Principal{}, store.ErrNotFound
	}
	storedRaw, err := secretbox.Open(item.KeyCiphertext, s.masterKey)
	if err != nil || storedRaw != rawKey {
		return Principal{}, store.ErrNotFound
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
