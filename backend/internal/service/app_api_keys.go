package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/platform/apikey"
	"github.com/zyf/chatapi/internal/store"
)

type AppAPIPrincipal struct {
	KeyID                string
	UserID               string
	Name                 string
	KeyPrefix            string
	Scopes               map[string]struct{}
	ResourceLimits       map[string]any
	AllowedActions       map[string]struct{}
	MaxRequestsPerMinute int
}

type AppAPIKeyService struct {
	store         store.Store
	rateLimitMu   sync.Mutex
	rateLimitHits map[string][]time.Time
}

const appAPIKeyLastUsedMinInterval = 5 * time.Minute
const maxIntValue = int(^uint(0) >> 1)

func NewAppAPIKeyService(dataStore store.Store) *AppAPIKeyService {
	return &AppAPIKeyService{store: dataStore, rateLimitHits: map[string][]time.Time{}}
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
	now := time.Now().UTC()
	if item.LastUsedAt == nil || now.Sub(*item.LastUsedAt) >= appAPIKeyLastUsedMinInterval {
		_ = s.store.UpdateAppAPIKeyLastUsedAt(ctx, item.ID, now)
	}
	principal := AppAPIPrincipal{
		KeyID:                item.ID,
		UserID:               item.UserID,
		Name:                 item.Name,
		KeyPrefix:            item.KeyPrefix,
		Scopes:               make(map[string]struct{}, len(item.Scopes)),
		ResourceLimits:       item.ResourceLimits,
		AllowedActions:       make(map[string]struct{}),
		MaxRequestsPerMinute: positiveInt(item.ResourceLimits["max_requests_per_minute"]),
	}
	for _, scope := range item.Scopes {
		principal.Scopes[strings.TrimSpace(scope)] = struct{}{}
	}
	for _, action := range stringArray(item.ResourceLimits["allowed_request_actions"]) {
		principal.AllowedActions[action] = struct{}{}
	}
	return principal, nil
}

func (s *AppAPIKeyService) AllowRequest(principal AppAPIPrincipal, now time.Time) bool {
	limit := principal.MaxRequestsPerMinute
	if limit <= 0 {
		return true
	}
	keyID := strings.TrimSpace(principal.KeyID)
	if keyID == "" {
		return false
	}
	cutoff := now.Add(-time.Minute)
	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()
	hits := s.rateLimitHits[keyID]
	kept := hits[:0]
	for _, hit := range hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= limit {
		s.rateLimitHits[keyID] = kept
		return false
	}
	kept = append(kept, now)
	s.rateLimitHits[keyID] = kept
	return true
}

func (s *AppAPIKeyService) ListKeysForUser(ctx context.Context, userID string) ([]store.AppAPIKey, error) {
	return s.store.ListAppAPIKeysByUser(ctx, strings.TrimSpace(userID))
}

func (s *AppAPIKeyService) RevokeKey(ctx context.Context, userID string, keyID string) error {
	return s.store.RevokeAppAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}

func (s *AppAPIKeyService) RecordAudit(ctx context.Context, principal AppAPIPrincipal, route string, statusCode int, errorCode string) {
	_ = s.store.CreateAppAPIKeyAuditLog(ctx, store.AppAPIKeyAuditLog{
		ID:          "applog_" + uuid.NewString(),
		AppAPIKeyID: principal.KeyID,
		UserID:      principal.UserID,
		Route:       strings.TrimSpace(route),
		StatusCode:  statusCode,
		ErrorCode:   strings.TrimSpace(errorCode),
		CreatedAt:   time.Now().UTC(),
	})
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

func positiveInt(value any) int {
	switch raw := value.(type) {
	case int:
		if raw > 0 {
			return raw
		}
	case int64:
		if raw > 0 && raw <= int64(maxIntValue) {
			return int(raw)
		}
	case float64:
		if raw > 0 && raw <= float64(maxIntValue) {
			return int(raw)
		}
	case json.Number:
		if parsed, err := raw.Int64(); err == nil && parsed > 0 && parsed <= int64(maxIntValue) {
			return int(parsed)
		}
	}
	return 0
}
