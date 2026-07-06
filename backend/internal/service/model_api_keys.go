package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/platform/apikey"
	"github.com/zyf/chatapi/internal/platform/secretbox"
	"github.com/zyf/chatapi/internal/store"
)

type ModelAPIPrincipal struct {
	KeyID     string
	UserID    string
	Name      string
	KeyPrefix string
	Model     string
}

type ModelAPIKeyService struct {
	store     store.Store
	masterKey string
}

type ModelAPIKeySchema struct {
	KeyPrefix    string                   `json:"key_prefix"`
	CreateFields []ModelAPIKeyCreateField `json:"create_fields"`
}

type ModelAPIKeyCreateField struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

const modelAPIKeyLastUsedMinInterval = 5 * time.Minute

var ErrModelRequired = errors.New("model is required")

func NewModelAPIKeyService(dataStore store.Store, masterKey string) *ModelAPIKeyService {
	return &ModelAPIKeyService{
		store:     dataStore,
		masterKey: strings.TrimSpace(masterKey),
	}
}

func (s *ModelAPIKeyService) Schema() ModelAPIKeySchema {
	return ModelAPIKeySchema{
		KeyPrefix: "sk-",
		CreateFields: []ModelAPIKeyCreateField{
			{Name: "name", Required: false, Description: "Display name for the virtual model key."},
			{Name: "model", Required: true, Description: "Virtual model name that the key is bound to."},
		},
	}
}

func (s *ModelAPIKeyService) CreateKey(ctx context.Context, userID string, name string, model string) (store.ModelAPIKey, string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return store.ModelAPIKey{}, "", ErrModelRequired
	}
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
		KeyPrefix:     apikey.Prefix(raw),
		Model:         model,
	})
	if err != nil {
		return store.ModelAPIKey{}, "", err
	}
	item.RawKey = raw
	return item, raw, nil
}

func (s *ModelAPIKeyService) Authenticate(ctx context.Context, rawKey string) (ModelAPIPrincipal, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return ModelAPIPrincipal{}, ErrForbidden
	}
	item, err := s.store.GetModelAPIKeyByPrefix(ctx, apikey.Prefix(rawKey))
	if err != nil {
		return ModelAPIPrincipal{}, ErrForbidden
	}
	if item.RevokedAt != nil {
		return ModelAPIPrincipal{}, ErrForbidden
	}
	storedRaw, err := secretbox.Open(item.KeyCiphertext, s.masterKey)
	if err != nil {
		return ModelAPIPrincipal{}, ErrForbidden
	}
	if storedRaw != rawKey {
		return ModelAPIPrincipal{}, ErrForbidden
	}
	now := time.Now().UTC()
	if item.LastUsedAt == nil || now.Sub(*item.LastUsedAt) >= modelAPIKeyLastUsedMinInterval {
		_ = s.store.UpdateModelAPIKeyLastUsedAt(ctx, item.ID, now)
	}
	return ModelAPIPrincipal{
		KeyID:     item.ID,
		UserID:    item.UserID,
		Name:      item.Name,
		KeyPrefix: item.KeyPrefix,
		Model:     item.Model,
	}, nil
}

func (s *ModelAPIKeyService) ListKeysForUser(ctx context.Context, userID string) ([]store.ModelAPIKey, error) {
	items, err := s.store.ListModelAPIKeysByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	for index := range items {
		if items[index].RevokedAt != nil {
			continue
		}
		raw, err := secretbox.Open(items[index].KeyCiphertext, s.masterKey)
		if err == nil {
			items[index].RawKey = raw
		}
	}
	return items, nil
}

func (s *ModelAPIKeyService) GetKeyForUser(ctx context.Context, userID string, keyID string) (store.ModelAPIKey, error) {
	item, err := s.store.GetModelAPIKeyByID(ctx, strings.TrimSpace(keyID))
	if err != nil {
		return store.ModelAPIKey{}, ErrPendingNotFound
	}
	if strings.TrimSpace(item.UserID) != strings.TrimSpace(userID) {
		return store.ModelAPIKey{}, ErrForbidden
	}
	if item.RevokedAt == nil {
		raw, err := secretbox.Open(item.KeyCiphertext, s.masterKey)
		if err == nil {
			item.RawKey = raw
		}
	}
	return item, nil
}

func (s *ModelAPIKeyService) RevokeKey(ctx context.Context, userID string, keyID string) error {
	return s.store.RevokeModelAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}
