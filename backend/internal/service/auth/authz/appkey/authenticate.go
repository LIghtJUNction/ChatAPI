package app

import (
	"context"
	"strings"
	"time"

	keyutil "github.com/zyf2007/ChatAPI/internal/platform/apikey"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Service) Authenticate(ctx context.Context, rawKey string) (Principal, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return Principal{}, common.ErrNotFound
	}
	item, err := s.store.GetAppAPIKeyByPrefix(ctx, keyutil.Prefix(rawKey))
	if err != nil || item.RevokedAt != nil {
		return Principal{}, common.ErrNotFound
	}
	if item.ExpiresAt != nil && item.ExpiresAt.Before(time.Now().UTC()) {
		return Principal{}, common.ErrNotFound
	}
	if !keyutil.Verify(rawKey, item.KeyHash) {
		return Principal{}, common.ErrNotFound
	}
	now := time.Now().UTC()
	if item.LastUsedAt == nil || now.Sub(*item.LastUsedAt) >= appLastUsedMinInterval {
		_ = s.store.UpdateAppAPIKeyLastUsedAt(ctx, item.ID, now)
	}
	principal := Principal{
		KeyID:                item.ID,
		UserID:               item.UserID,
		Name:                 item.Name,
		KeyPrefix:            item.KeyPrefix,
		Scopes:               map[string]struct{}{},
		ResourceLimits:       cloneMap(item.ResourceLimits),
		AllowedActions:       map[string]struct{}{},
		MaxRequestsPerMinute: positiveInt(item.ResourceLimits["max_requests_per_minute"]),
		AllowedSourceIPs:     stringArray(item.ResourceLimits["allowed_source_ips"]),
	}
	for _, scope := range item.Scopes {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			principal.Scopes[scope] = struct{}{}
		}
	}
	for _, action := range stringArray(item.ResourceLimits["allowed_request_actions"]) {
		principal.AllowedActions[action] = struct{}{}
	}
	return principal, nil
}
