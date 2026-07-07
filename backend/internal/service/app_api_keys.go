package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
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
	AllowedSourceIPs     []string
}

type AppAPIKeyService struct {
	store         store.Store
	rateLimitMu   sync.Mutex
	rateLimitHits map[string][]time.Time
}

type AppAPIKeySchema struct {
	Authentication       AppAPIAuthenticationSchema         `json:"authentication"`
	Scopes               []AppAPIKeyScopeSpec               `json:"scopes"`
	ResourceLimits       []AppAPIKeyResourceLimitSpec       `json:"resource_limits"`
	ResourceLimitBinding []AppAPIResourceLimitBindingSchema `json:"resource_limit_bindings,omitempty"`
	Operations           []AppAPIOperationContract          `json:"operations,omitempty"`
	ErrorCodes           []AppAPIErrorCodeSchema            `json:"error_codes,omitempty"`
}

type AppAPIKeyScopeSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type AppAPIKeyResourceLimitSpec struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	ValueType         string   `json:"value_type"`
	RequiresAnyScopes []string `json:"requires_any_scopes,omitempty"`
	AllowedValues     []string `json:"allowed_values,omitempty"`
}

const appAPIKeyLastUsedMinInterval = 5 * time.Minute
const maxIntValue = int(^uint(0) >> 1)

var ErrInvalidAppAPIKeyExpiry = errors.New("app api key expires_at must be in the future")

var supportedAppAPIScopes = map[string]struct{}{
	"requests:read":      {},
	"requests:respond":   {},
	"conversations:read": {},
	"automation:read":    {},
	"automation:write":   {},
	"model_keys:read":    {},
	"model_keys:write":   {},
	"model_keys:delete":  {},
	"statistics:read":    {},
}

var supportedAppAPIRequestActions = map[string]struct{}{
	"delta":    {},
	"complete": {},
	"abort":    {},
}

type AppAPIKeyConfigError struct {
	message string
}

func (e *AppAPIKeyConfigError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

func NewAppAPIKeyService(dataStore store.Store) *AppAPIKeyService {
	return &AppAPIKeyService{store: dataStore, rateLimitHits: map[string][]time.Time{}}
}

func (s *AppAPIKeyService) Schema() AppAPIKeySchema {
	return BuildAppAPIKeySchema()
}

func (s *AppAPIKeyService) CreateKey(ctx context.Context, userID string, name string, scopes []string, resourceLimits map[string]any, expiresAt *time.Time) (store.AppAPIKey, string, error) {
	if expiresAt != nil && !expiresAt.After(time.Now().UTC()) {
		return store.AppAPIKey{}, "", ErrInvalidAppAPIKeyExpiry
	}
	scopes, resourceLimits, err := normalizeAppAPIKeyConfig(scopes, resourceLimits)
	if err != nil {
		return store.AppAPIKey{}, "", err
	}
	raw := "ak-" + uuid.NewString()
	item, err := s.store.CreateAppAPIKey(ctx, store.CreateAppAPIKeyInput{
		ID:             "appkey_" + uuid.NewString(),
		UserID:         strings.TrimSpace(userID),
		Name:           strings.TrimSpace(name),
		KeyHash:        apikey.Hash(raw),
		KeyPrefix:      apikey.Prefix(raw),
		Scopes:         scopes,
		ResourceLimits: resourceLimits,
		ExpiresAt:      expiresAt,
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
		AllowedSourceIPs:     stringArray(item.ResourceLimits["allowed_source_ips"]),
	}
	for _, scope := range item.Scopes {
		principal.Scopes[strings.TrimSpace(scope)] = struct{}{}
	}
	for _, action := range stringArray(item.ResourceLimits["allowed_request_actions"]) {
		principal.AllowedActions[action] = struct{}{}
	}
	return principal, nil
}

func (s *AppAPIKeyService) AllowSourceIP(principal AppAPIPrincipal, remoteAddr string) bool {
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

func normalizeAppAPIKeyConfig(scopes []string, resourceLimits map[string]any) ([]string, map[string]any, error) {
	normalizedScopes, scopeSet, err := normalizeAppAPIKeyScopes(scopes)
	if err != nil {
		return nil, nil, err
	}
	normalizedLimits, err := normalizeAppAPIResourceLimits(resourceLimits, scopeSet)
	if err != nil {
		return nil, nil, err
	}
	return normalizedScopes, normalizedLimits, nil
}

func BuildAppAPIKeySchema() AppAPIKeySchema {
	return AppAPIKeySchema{
		Authentication: BuildAppAPIAuthenticationSchema(),
		Scopes: []AppAPIKeyScopeSpec{
			{Name: "requests:read", Description: "Read current user's pending and historical requests."},
			{Name: "requests:respond", Description: "Append delta, complete, or abort a request."},
			{Name: "conversations:read", Description: "Read current user's conversations and messages."},
			{Name: "automation:read", Description: "Read current user's automation rules."},
			{Name: "automation:write", Description: "Replace current user's automation rules."},
			{Name: "model_keys:read", Description: "List current user's virtual model API keys."},
			{Name: "model_keys:write", Description: "Create current user's virtual model API keys."},
			{Name: "model_keys:delete", Description: "Delete current user's virtual model API keys."},
			{Name: "statistics:read", Description: "Read current user's statistics summary."},
		},
		ResourceLimits: []AppAPIKeyResourceLimitSpec{
			{Name: "allowed_model_key_ids", Description: "Restrict model key operations to specific virtual model key IDs.", ValueType: "string_array", RequiresAnyScopes: []string{"model_keys:read", "model_keys:delete"}},
			{Name: "allowed_request_ids", Description: "Restrict request reads and responses to specific request IDs.", ValueType: "string_array", RequiresAnyScopes: []string{"requests:read", "requests:respond"}},
			{Name: "allowed_conversation_ids", Description: "Restrict conversation reads to specific conversation IDs.", ValueType: "string_array", RequiresAnyScopes: []string{"conversations:read", "requests:read", "requests:respond"}},
			{Name: "allowed_virtual_models", Description: "Restrict accessible requests, conversations, or model-key creation to specific virtual models.", ValueType: "string_array", RequiresAnyScopes: []string{"requests:read", "requests:respond", "conversations:read", "model_keys:write"}},
			{Name: "allowed_automation_rule_ids", Description: "Restrict automation rule operations to specific rule IDs.", ValueType: "string_array", RequiresAnyScopes: []string{"automation:read", "automation:write"}},
			{Name: "allowed_request_actions", Description: "Restrict request response actions to a subset of delta, complete, abort.", ValueType: "string_array", RequiresAnyScopes: []string{"requests:respond"}, AllowedValues: []string{"delta", "complete", "abort"}},
			{Name: "max_requests_per_minute", Description: "Apply a per-key request rate limit in requests per minute.", ValueType: "positive_integer"},
			{Name: "max_model_keys", Description: "Limit how many active virtual model keys the key holder may create.", ValueType: "positive_integer", RequiresAnyScopes: []string{"model_keys:write"}},
			{Name: "allowed_source_ips", Description: "Restrict requests to exact IPs or CIDR blocks.", ValueType: "string_array"},
		},
		ResourceLimitBinding: []AppAPIResourceLimitBindingSchema{
			{Name: "allowed_model_key_ids", AffectsOperations: []string{"list_model_keys", "delete_model_key"}, Behavior: "Only the listed virtual model key ids remain visible or deletable."},
			{Name: "allowed_request_ids", AffectsOperations: []string{"list_requests", "get_request", "copy_request_curl", "request_delta", "request_complete", "request_abort"}, Behavior: "Only the listed request ids remain readable and mutable."},
			{Name: "allowed_conversation_ids", AffectsOperations: []string{"list_requests", "get_request", "copy_request_curl", "request_delta", "request_complete", "request_abort", "list_conversations", "list_conversation_messages"}, Behavior: "Requests and conversations are filtered to the listed conversation ids."},
			{Name: "allowed_virtual_models", AffectsOperations: []string{"list_requests", "get_request", "copy_request_curl", "request_delta", "request_complete", "request_abort", "list_conversations", "list_conversation_messages", "create_model_key"}, Behavior: "Only requests and conversations belonging to the listed virtual model names remain visible; model-key creation is also limited to them."},
			{Name: "allowed_automation_rule_ids", AffectsOperations: []string{"list_automation_rules", "replace_automation_rules"}, Behavior: "Automation reads and writes are limited to the listed rule ids."},
			{Name: "allowed_request_actions", AffectsOperations: []string{"request_delta", "request_complete", "request_abort"}, Behavior: "Only the listed turn-control actions may be used even when requests:respond is present."},
			{Name: "max_requests_per_minute", AffectsOperations: []string{"me", "list_requests", "get_request", "copy_request_curl", "list_conversations", "list_conversation_messages", "list_automation_rules", "replace_automation_rules", "statistics_summary", "list_model_keys", "create_model_key", "delete_model_key", "request_delta", "request_complete", "request_abort"}, Behavior: "All /api/app/* routes share the same per-key one-minute request budget."},
			{Name: "max_model_keys", AffectsOperations: []string{"create_model_key"}, Behavior: "Create model-key requests are rejected once the number of active virtual model keys reaches the configured cap."},
			{Name: "allowed_source_ips", AffectsOperations: []string{"me", "list_requests", "get_request", "copy_request_curl", "list_conversations", "list_conversation_messages", "list_automation_rules", "replace_automation_rules", "statistics_summary", "list_model_keys", "create_model_key", "delete_model_key", "request_delta", "request_complete", "request_abort"}, Behavior: "Requests are rejected before scope checks unless the caller IP matches an exact IP or CIDR entry."},
		},
		Operations: []AppAPIOperationContract{
			{Name: "me", Method: "GET", Path: "/api/app/me", Description: "Read the current application API key metadata and its owner id.", RequiredScopes: []string{"requests:read"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "internal_error"), ResponseShape: "{ok, app_api_key, user}", ConsumesRateLimit: true},
			{Name: "list_requests", Method: "GET", Path: "/api/app/requests", Description: "List requests visible to the key owner together with parsed summary items.", RequiredScopes: []string{"requests:read"}, ResourceLimitKeys: []string{"allowed_request_ids", "allowed_conversation_ids", "allowed_virtual_models"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "internal_error"), ResponseShape: "{ok, items, parsed_items}", ConsumesRateLimit: true},
			{Name: "get_request", Method: "GET", Path: "/api/app/requests/{request_id}", Description: "Read one captured request and its parsed detail view.", RequiredScopes: []string{"requests:read"}, ResourceLimitKeys: []string{"allowed_request_ids", "allowed_conversation_ids", "allowed_virtual_models"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "not_found", "internal_error"), ResponseShape: "{ok, request, parsed}", ConsumesRateLimit: true},
			{Name: "copy_request_curl", Method: "POST", Path: "/api/app/requests/{request_id}/copy-curl", Description: "Build a replayable curl command for one captured request.", RequiredScopes: []string{"requests:read"}, ResourceLimitKeys: []string{"allowed_request_ids", "allowed_conversation_ids", "allowed_virtual_models"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "not_found", "internal_error"), ResponseShape: "{ok, request_id, curl}", ConsumesRateLimit: true},
			{Name: "request_delta", Method: "POST", Path: "/api/app/requests/{request_id}/delta", Description: "Persist a draft delta for a pending request without completing it.", RequiredScopes: []string{"requests:respond"}, ResourceLimitKeys: []string{"allowed_request_ids", "allowed_conversation_ids", "allowed_virtual_models", "allowed_request_actions"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "invalid_json_body", "pending_not_found", "pending_conflict", "internal_error"), ResponseShape: "{ok, ...turn_control_result}", ConsumesRateLimit: true},
			{Name: "request_complete", Method: "POST", Path: "/api/app/requests/{request_id}/complete", Description: "Complete a pending request with assistant text, thinking, tool call, or tool result payload.", RequiredScopes: []string{"requests:respond"}, ResourceLimitKeys: []string{"allowed_request_ids", "allowed_conversation_ids", "allowed_virtual_models", "allowed_request_actions"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "invalid_json_body", "pending_not_found", "pending_conflict", "internal_error"), ResponseShape: "{ok, ...turn_control_result}", ConsumesRateLimit: true},
			{Name: "request_abort", Method: "POST", Path: "/api/app/requests/{request_id}/abort", Description: "Abort a pending request and return an error payload to the waiting client.", RequiredScopes: []string{"requests:respond"}, ResourceLimitKeys: []string{"allowed_request_ids", "allowed_conversation_ids", "allowed_virtual_models", "allowed_request_actions"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "invalid_json_body", "pending_not_found", "pending_conflict", "internal_error"), ResponseShape: "{ok, ...turn_control_result}", ConsumesRateLimit: true},
			{Name: "list_conversations", Method: "GET", Path: "/api/app/conversations", Description: "List conversations visible to the key owner.", RequiredScopes: []string{"conversations:read"}, ResourceLimitKeys: []string{"allowed_conversation_ids", "allowed_virtual_models"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "internal_error"), ResponseShape: "{ok, items}", ConsumesRateLimit: true},
			{Name: "list_conversation_messages", Method: "GET", Path: "/api/app/conversations/{conversation_id}/messages", Description: "List messages of a visible conversation.", RequiredScopes: []string{"conversations:read"}, ResourceLimitKeys: []string{"allowed_conversation_ids", "allowed_virtual_models"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "not_found", "internal_error"), ResponseShape: "{ok, items}", ConsumesRateLimit: true},
			{Name: "list_automation_rules", Method: "GET", Path: "/api/app/automation-rules", Description: "List automation rules visible to the key owner.", RequiredScopes: []string{"automation:read"}, ResourceLimitKeys: []string{"allowed_automation_rule_ids"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "internal_error"), ResponseShape: "{ok, rules}", ConsumesRateLimit: true},
			{Name: "replace_automation_rules", Method: "PUT", Path: "/api/app/automation-rules", Description: "Replace the visible automation rule set for the key owner.", RequiredScopes: []string{"automation:write"}, ResourceLimitKeys: []string{"allowed_automation_rule_ids"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "invalid_json_body", "invalid_request", "internal_error"), ResponseShape: "{ok, rules}", ConsumesRateLimit: true},
			{Name: "statistics_summary", Method: "GET", Path: "/api/app/statistics/summary", Description: "Read request statistics summary for the key owner.", RequiredScopes: []string{"statistics:read"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "internal_error"), ResponseShape: "{ok, summary}", ConsumesRateLimit: true},
			{Name: "list_model_keys", Method: "GET", Path: "/api/app/model-keys", Description: "List virtual model API keys visible to the key owner.", RequiredScopes: []string{"model_keys:read"}, ResourceLimitKeys: []string{"allowed_model_key_ids"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "internal_error"), ResponseShape: "{ok, items}", ConsumesRateLimit: true},
			{Name: "create_model_key", Method: "POST", Path: "/api/app/model-keys", Description: "Create a new virtual model API key for the key owner.", RequiredScopes: []string{"model_keys:write"}, ResourceLimitKeys: []string{"allowed_virtual_models", "max_model_keys"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "invalid_json_body", "invalid_request", "internal_error"), ResponseShape: "{ok, item, raw_key}", ConsumesRateLimit: true},
			{Name: "delete_model_key", Method: "DELETE", Path: "/api/app/model-keys/{key_id}", Description: "Revoke one visible virtual model API key.", RequiredScopes: []string{"model_keys:delete"}, ResourceLimitKeys: []string{"allowed_model_key_ids"}, ErrorCodes: appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "not_found", "internal_error"), ResponseShape: "{ok}", ConsumesRateLimit: true},
		},
		ErrorCodes: BuildCommonAppAPIErrorCodes(),
	}
}

func normalizeAppAPIKeyScopes(scopes []string) ([]string, map[string]struct{}, error) {
	if len(scopes) == 0 {
		return nil, nil, &AppAPIKeyConfigError{message: "app api key scopes are required"}
	}
	normalized := make([]string, 0, len(scopes))
	scopeSet := make(map[string]struct{}, len(scopes))
	for _, raw := range scopes {
		scope := strings.TrimSpace(raw)
		if scope == "" {
			continue
		}
		if _, ok := supportedAppAPIScopes[scope]; !ok {
			return nil, nil, &AppAPIKeyConfigError{message: "unsupported app api key scope: " + scope}
		}
		if _, exists := scopeSet[scope]; exists {
			continue
		}
		scopeSet[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	if len(normalized) == 0 {
		return nil, nil, &AppAPIKeyConfigError{message: "app api key scopes are required"}
	}
	return normalized, scopeSet, nil
}

func normalizeAppAPIResourceLimits(resourceLimits map[string]any, scopes map[string]struct{}) (map[string]any, error) {
	if len(resourceLimits) == 0 {
		return map[string]any{}, nil
	}
	normalized := make(map[string]any, len(resourceLimits))
	for key, rawValue := range resourceLimits {
		switch strings.TrimSpace(key) {
		case "allowed_model_key_ids", "allowed_request_ids", "allowed_conversation_ids", "allowed_virtual_models", "allowed_automation_rule_ids":
			items, err := normalizeStringList(rawValue, key)
			if err != nil {
				return nil, err
			}
			if err := validateAppAPIResourceLimitScope(key, scopes); err != nil {
				return nil, err
			}
			normalized[key] = items
		case "allowed_request_actions":
			items, err := normalizeStringList(rawValue, key)
			if err != nil {
				return nil, err
			}
			if _, ok := scopes["requests:respond"]; !ok {
				return nil, &AppAPIKeyConfigError{message: "allowed_request_actions requires requests:respond scope"}
			}
			for _, item := range items {
				if _, ok := supportedAppAPIRequestActions[item]; !ok {
					return nil, &AppAPIKeyConfigError{message: "unsupported allowed_request_action: " + item}
				}
			}
			normalized[key] = items
		case "max_requests_per_minute", "max_model_keys":
			value := positiveInt(rawValue)
			if value <= 0 {
				return nil, &AppAPIKeyConfigError{message: key + " must be a positive integer"}
			}
			if err := validateAppAPIResourceLimitScope(key, scopes); err != nil {
				return nil, err
			}
			normalized[key] = value
		case "allowed_source_ips":
			items, err := normalizeStringList(rawValue, key)
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				if !isValidIPOrCIDR(item) {
					return nil, &AppAPIKeyConfigError{message: "invalid allowed_source_ips entry: " + item}
				}
			}
			normalized[key] = items
		default:
			return nil, &AppAPIKeyConfigError{message: "unsupported app api key resource limit: " + strings.TrimSpace(key)}
		}
	}
	return normalized, nil
}

func normalizeStringList(value any, field string) ([]string, error) {
	switch raw := value.(type) {
	case []string:
		return dedupeNonEmptyStrings(raw), nil
	case []any:
		items := make([]string, 0, len(raw))
		for _, item := range raw {
			text, ok := item.(string)
			if !ok {
				return nil, &AppAPIKeyConfigError{message: field + " must be an array of strings"}
			}
			text = strings.TrimSpace(text)
			if text != "" {
				items = append(items, text)
			}
		}
		return dedupeNonEmptyStrings(items), nil
	case nil:
		return []string{}, nil
	default:
		return nil, &AppAPIKeyConfigError{message: field + " must be an array of strings"}
	}
}

func dedupeNonEmptyStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(items))
	normalized := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

func isValidIPOrCIDR(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "/") {
		_, err := netip.ParsePrefix(value)
		return err == nil
	}
	_, err := netip.ParseAddr(value)
	return err == nil
}

func validateAppAPIResourceLimitScope(key string, scopes map[string]struct{}) error {
	if len(scopes) == 0 {
		return &AppAPIKeyConfigError{message: key + " requires matching scope"}
	}
	requiredAny := map[string][]string{
		"allowed_model_key_ids":       {"model_keys:read", "model_keys:delete"},
		"allowed_request_ids":         {"requests:read", "requests:respond"},
		"allowed_conversation_ids":    {"conversations:read", "requests:read", "requests:respond"},
		"allowed_virtual_models":      {"requests:read", "requests:respond", "conversations:read", "model_keys:write"},
		"allowed_automation_rule_ids": {"automation:read", "automation:write"},
		"max_model_keys":              {"model_keys:write"},
	}
	allowedScopes, ok := requiredAny[key]
	if !ok {
		return nil
	}
	for _, scope := range allowedScopes {
		if _, ok := scopes[scope]; ok {
			return nil
		}
	}
	return &AppAPIKeyConfigError{message: key + " requires one of scopes: " + strings.Join(allowedScopes, ", ")}
}

func (s *AppAPIKeyService) ValidateConfig(scopes []string, resourceLimits map[string]any) error {
	_, _, err := normalizeAppAPIKeyConfig(scopes, resourceLimits)
	return err
}
