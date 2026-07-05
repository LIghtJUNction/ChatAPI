package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/platform/apikey"
	"github.com/zyf/chatapi/internal/store"
)

type AppAPIPrincipal struct {
	KeyID          string
	UserID         string
	Name           string
	KeyPrefix      string
	Scopes         map[string]struct{}
	ResourceLimits map[string]any
	AllowedActions map[string]struct{}
}

type AppAPIKeyService struct {
	store store.Store
}

func NewAppAPIKeyService(dataStore store.Store) *AppAPIKeyService {
	return &AppAPIKeyService{store: dataStore}
}

func (s *AppAPIKeyService) CreateKey(ctx context.Context, userID string, name string, scopes []string, resourceLimits map[string]any) (store.AppAPIKey, string, error) {
	raw := "ak-" + uuid.NewString()
	item, err := s.store.CreateAppAPIKey(ctx, store.CreateAppAPIKeyInput{
		ID:             "appkey_" + uuid.NewString(),
		UserID:         strings.TrimSpace(userID),
		Name:           strings.TrimSpace(name),
		KeyHash:        apikey.Hash(raw),
		KeyPrefix:      apikey.Prefix(raw),
		Scopes:         scopes,
		ResourceLimits: resourceLimits,
	})
	if err != nil {
		return store.AppAPIKey{}, "", err
	}
	return item, raw, nil
}

func (s *AppAPIKeyService) Authenticate(ctx context.Context, rawKey string) (AppAPIPrincipal, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return AppAPIPrincipal{}, ErrForbidden
	}
	item, err := s.store.GetAppAPIKeyByPrefix(ctx, apikey.Prefix(rawKey))
	if err != nil {
		return AppAPIPrincipal{}, ErrForbidden
	}
	if item.RevokedAt != nil {
		return AppAPIPrincipal{}, ErrForbidden
	}
	if item.ExpiresAt != nil && item.ExpiresAt.Before(time.Now().UTC()) {
		return AppAPIPrincipal{}, ErrForbidden
	}
	if !apikey.Verify(rawKey, item.KeyHash) {
		return AppAPIPrincipal{}, ErrForbidden
	}
	_ = s.store.UpdateAppAPIKeyLastUsedAt(ctx, item.ID, time.Now().UTC())
	principal := AppAPIPrincipal{
		KeyID:          item.ID,
		UserID:         item.UserID,
		Name:           item.Name,
		KeyPrefix:      item.KeyPrefix,
		Scopes:         make(map[string]struct{}, len(item.Scopes)),
		ResourceLimits: item.ResourceLimits,
		AllowedActions: make(map[string]struct{}),
	}
	for _, scope := range item.Scopes {
		principal.Scopes[strings.TrimSpace(scope)] = struct{}{}
	}
	for _, action := range stringArray(item.ResourceLimits["allowed_request_actions"]) {
		principal.AllowedActions[action] = struct{}{}
	}
	return principal, nil
}

func stringArray(value any) []string {
	switch raw := value.(type) {
	case []string:
		return raw
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			text, _ := item.(string)
			text = strings.TrimSpace(text)
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
