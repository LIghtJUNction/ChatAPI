package app

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/zyf/chatapi/internal/observability/logging"
	keyutil "github.com/zyf/chatapi/internal/platform/apikey"
	"github.com/zyf/chatapi/internal/store"
)

type Principal struct {
	KeyID                string
	UserID               string
	Name                 string
	KeyPrefix            string
	Scopes               map[string]struct{}
	ResourceLimits       map[string]any
	AllowedActions       map[string]struct{}
	MaxRequestsPerMinute int
	AllowedSourceIPs     []string
}

type Service struct {
	store         store.Store
	rateLimitMu   sync.Mutex
	rateLimitHits map[string][]time.Time
	Logger        *zap.Logger
}

const appLastUsedMinInterval = 5 * time.Minute

func NewService(dataStore store.Store) *Service {
	return &Service{store: dataStore, rateLimitHits: map[string][]time.Time{}}
}

func (s *Service) CreateKey(ctx context.Context, userID string, name string, scopes []string, resourceLimits map[string]any, expiresAt *time.Time) (store.AppAPIKey, string, error) {
	raw := "ak-" + uuid.NewString()
	item, err := s.store.CreateAppAPIKey(ctx, store.CreateAppAPIKeyInput{
		ID:             "appkey_" + uuid.NewString(),
		UserID:         strings.TrimSpace(userID),
		Name:           strings.TrimSpace(name),
		KeyHash:        keyutil.Hash(raw),
		KeyPrefix:      keyutil.Prefix(raw),
		Scopes:         append([]string(nil), scopes...),
		ResourceLimits: cloneMap(resourceLimits),
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		return store.AppAPIKey{}, "", err
	}
	return item, raw, nil
}

func (s *Service) Authenticate(ctx context.Context, rawKey string) (Principal, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return Principal{}, store.ErrNotFound
	}
	item, err := s.store.GetAppAPIKeyByPrefix(ctx, keyutil.Prefix(rawKey))
	if err != nil || item.RevokedAt != nil {
		return Principal{}, store.ErrNotFound
	}
	if item.ExpiresAt != nil && item.ExpiresAt.Before(time.Now().UTC()) {
		return Principal{}, store.ErrNotFound
	}
	if !keyutil.Verify(rawKey, item.KeyHash) {
		return Principal{}, store.ErrNotFound
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

func (s *Service) AllowSourceIP(principal Principal, remoteAddr string) bool {
	if len(principal.AllowedSourceIPs) == 0 {
		return true
	}
	host := strings.TrimSpace(remoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, rawRule := range principal.AllowedSourceIPs {
		rule := strings.TrimSpace(rawRule)
		if rule == "" {
			continue
		}
		if strings.Contains(rule, "/") {
			prefix, err := netip.ParsePrefix(rule)
			if err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		allowedAddr, err := netip.ParseAddr(rule)
		if err == nil && allowedAddr == addr {
			return true
		}
	}
	return false
}

func (s *Service) AllowRequest(principal Principal, now time.Time) bool {
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

func (s *Service) RecordAudit(ctx context.Context, principal Principal, route string, statusCode int, errorCode string) {
	item := store.AppAPIKeyAuditLog{
		ID:          "applog_" + uuid.NewString(),
		AppAPIKeyID: principal.KeyID,
		UserID:      principal.UserID,
		Route:       strings.TrimSpace(route),
		StatusCode:  statusCode,
		ErrorCode:   strings.TrimSpace(errorCode),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.CreateAppAPIKeyAuditLog(ctx, item); err != nil {
		logging.BindContext(s.Logger, ctx,
			zap.String("audit.kind", "app_api_key"),
			zap.String("app_api_key.id", principal.KeyID),
			zap.String("route", item.Route),
			zap.Int("http.status_code", statusCode),
			zap.String("error.code", item.ErrorCode),
		).Warn("failed to write app api key audit log", zap.Error(err))
		return
	}
	logging.BindContext(s.Logger, ctx,
		zap.String("audit.kind", "app_api_key"),
		zap.String("app_api_key.id", principal.KeyID),
		zap.String("route", item.Route),
		zap.Int("http.status_code", statusCode),
		zap.String("error.code", item.ErrorCode),
	).Debug("wrote app api key audit log")
}

func stringArray(value any) []string {
	switch raw := value.(type) {
	case []string:
		return append([]string(nil), raw...)
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
		if raw > 0 {
			return int(raw)
		}
	case float64:
		if raw > 0 {
			return int(raw)
		}
	}
	return 0
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
