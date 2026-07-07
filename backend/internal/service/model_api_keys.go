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
	KeyPrefix            string                             `json:"key_prefix"`
	CreateFields         []ModelAPIKeyCreateField           `json:"create_fields"`
	Authentication       AppAPIAuthenticationSchema         `json:"authentication,omitempty"`
	ResourceLimitKeys    []string                           `json:"resource_limit_keys,omitempty"`
	Operations           []AppAPIOperationContract          `json:"operations,omitempty"`
	ErrorCodes           []AppAPIErrorCodeSchema            `json:"error_codes,omitempty"`
	ResourceLimitBinding []AppAPIResourceLimitBindingSchema `json:"resource_limit_bindings,omitempty"`
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

func (s *ModelAPIKeyService) AppSchema() ModelAPIKeySchema {
	schema := s.Schema()
	schema.Authentication = BuildAppAPIAuthenticationSchema()
	schema.ResourceLimitKeys = []string{"allowed_model_key_ids", "allowed_virtual_models", "max_model_keys"}
	schema.Operations = []AppAPIOperationContract{
		{Name: "list_model_keys", Method: "GET", Path: "/api/app/model-keys", Description: "List virtual model API keys visible to the application API key owner.", RequiredScopes: []string{"model_keys:read"}, ResourceLimitKeys: []string{"allowed_model_key_ids"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "internal_error"), ResponseShape: "{ok, items}", ConsumesRateLimit: true},
		{Name: "create_model_key", Method: "POST", Path: "/api/app/model-keys", Description: "Create a new virtual model API key and return the raw key once.", RequiredScopes: []string{"model_keys:write"}, ResourceLimitKeys: []string{"allowed_virtual_models", "max_model_keys"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "invalid_json_body", "invalid_request", "internal_error"), ResponseShape: "{ok, item, raw_key}", ConsumesRateLimit: true},
		{Name: "delete_model_key", Method: "DELETE", Path: "/api/app/model-keys/{key_id}", Description: "Revoke one visible virtual model API key.", RequiredScopes: []string{"model_keys:delete"}, ResourceLimitKeys: []string{"allowed_model_key_ids"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "not_found", "internal_error"), ResponseShape: "{ok}", ConsumesRateLimit: true},
	}
	schema.ResourceLimitBinding = []AppAPIResourceLimitBindingSchema{
		{Name: "allowed_model_key_ids", AffectsOperations: []string{"list_model_keys", "delete_model_key"}, Behavior: "Only the listed virtual model key ids remain visible or deletable."},
		{Name: "allowed_virtual_models", AffectsOperations: []string{"create_model_key"}, Behavior: "Create operations may only mint keys bound to the listed virtual model names."},
		{Name: "max_model_keys", AffectsOperations: []string{"create_model_key"}, Behavior: "Create operations are rejected once the number of active virtual model keys reaches the configured cap."},
	}
	schema.ErrorCodes = BuildCommonAppAPIErrorCodes()
	return schema
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
