package access

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

const systemAccessSettingsKey = "system_access_settings"

var ErrInvalidAccessSettings = errors.New("invalid access settings")

type Settings struct {
	GlobalRateLimitRequests   int           `json:"global_rate_limit_requests"`
	GlobalRateLimitWindow     time.Duration `json:"global_rate_limit_window"`
	UserRateLimitRequests     int           `json:"user_rate_limit_requests"`
	UserRateLimitWindow       time.Duration `json:"user_rate_limit_window"`
	SessionRateLimitRequests  int           `json:"session_rate_limit_requests"`
	SessionRateLimitWindow    time.Duration `json:"session_rate_limit_window"`
	AppKeyRateLimitRequests   int           `json:"app_key_rate_limit_requests"`
	AppKeyRateLimitWindow     time.Duration `json:"app_key_rate_limit_window"`
	ModelKeyRateLimitRequests int           `json:"model_key_rate_limit_requests"`
	ModelKeyRateLimitWindow   time.Duration `json:"model_key_rate_limit_window"`
}

type SettingsService struct {
	store       store.Store
	defaults    Settings
	cacheTTL    time.Duration
	mu          sync.RWMutex
	cachedAt    time.Time
	cachedValue Settings
}

type FieldSchema struct {
	Key          string         `json:"key"`
	Type         string         `json:"type"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	DefaultValue any            `json:"default_value"`
	Example      any            `json:"example,omitempty"`
	Minimum      *int           `json:"minimum,omitempty"`
	RequiredWith string         `json:"required_with,omitempty"`
	Enum         []string       `json:"enum,omitempty"`
	Meta         map[string]any `json:"meta,omitempty"`
}

type ResponseSchema struct {
	Key            string        `json:"key"`
	Title          string        `json:"title"`
	Description    string        `json:"description"`
	Fields         []FieldSchema `json:"fields"`
	SupportsPatch  bool          `json:"supports_patch"`
	UpdateStrategy string        `json:"update_strategy"`
}

func NewSettingsService(dataStore store.Store, defaults Settings) *SettingsService {
	return &SettingsService{
		store:    dataStore,
		defaults: defaults,
		cacheTTL: 3 * time.Second,
	}
}

func (s *SettingsService) Get(ctx context.Context) (Settings, error) {
	if s == nil || s.store == nil {
		return Settings{}, ErrInvalidAccessSettings
	}
	s.mu.RLock()
	if !s.cachedAt.IsZero() && time.Since(s.cachedAt) < s.cacheTTL {
		value := s.cachedValue
		s.mu.RUnlock()
		return value, nil
	}
	s.mu.RUnlock()

	value, err := s.load(ctx)
	if err != nil {
		return Settings{}, err
	}
	s.mu.Lock()
	s.cachedValue = value
	s.cachedAt = time.Now().UTC()
	s.mu.Unlock()
	return value, nil
}

func (s *SettingsService) GetMap(ctx context.Context) (map[string]any, error) {
	value, err := s.Get(ctx)
	if err != nil {
		return nil, err
	}
	return settingsMap(value), nil
}

func (s *SettingsService) GetDocument(ctx context.Context) (map[string]any, error) {
	current, err := s.GetMap(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"key":              systemAccessSettingsKey,
		"title":            "Access Settings",
		"description":      "Runtime-configurable access admission and rate limit settings used by anonymous and authenticated request gates.",
		"ok":               true,
		"current":          current,
		"schema":           s.Schema(),
		"response_version": 1,
	}, nil
}

func (s *SettingsService) Schema() ResponseSchema {
	defaults := s.defaults
	zero := 0
	return ResponseSchema{
		Key:            systemAccessSettingsKey,
		Title:          "Access Settings",
		Description:    "Access policy knobs for anonymous traffic and principal-aware request admission.",
		SupportsPatch:  true,
		UpdateStrategy: "merge",
		Fields: []FieldSchema{
			{
				Key:          "global_rate_limit_requests",
				Type:         "integer",
				Title:        "Global request limit",
				Description:  "Maximum anonymous requests allowed per source identity during the paired global rate limit window. Set 0 to disable.",
				DefaultValue: defaults.GlobalRateLimitRequests,
				Example:      defaults.GlobalRateLimitRequests,
				Minimum:      &zero,
				RequiredWith: "global_rate_limit_window",
			},
			{
				Key:          "global_rate_limit_window",
				Type:         "duration",
				Title:        "Global request window",
				Description:  "Duration string for anonymous/global rate limiting, for example 1m or 30s.",
				DefaultValue: defaults.GlobalRateLimitWindow.String(),
				Example:      "1m",
				RequiredWith: "global_rate_limit_requests",
				Meta:         map[string]any{"format": "go_duration"},
			},
			{
				Key:          "user_rate_limit_requests",
				Type:         "integer",
				Title:        "Per-user request limit",
				Description:  "Maximum requests per authenticated owner user across all that user's principals during the paired window. Set 0 to disable.",
				DefaultValue: defaults.UserRateLimitRequests,
				Example:      60,
				Minimum:      &zero,
				RequiredWith: "user_rate_limit_window",
			},
			{
				Key:          "user_rate_limit_window",
				Type:         "duration",
				Title:        "Per-user request window",
				Description:  "Duration string for per-user limiting.",
				DefaultValue: defaults.UserRateLimitWindow.String(),
				Example:      "1m",
				RequiredWith: "user_rate_limit_requests",
				Meta:         map[string]any{"format": "go_duration"},
			},
			{
				Key:          "session_rate_limit_requests",
				Type:         "integer",
				Title:        "Per-session request limit",
				Description:  "Maximum requests for a single browser/session principal during the paired window. Set 0 to disable.",
				DefaultValue: defaults.SessionRateLimitRequests,
				Example:      30,
				Minimum:      &zero,
				RequiredWith: "session_rate_limit_window",
			},
			{
				Key:          "session_rate_limit_window",
				Type:         "duration",
				Title:        "Per-session request window",
				Description:  "Duration string for per-session limiting.",
				DefaultValue: defaults.SessionRateLimitWindow.String(),
				Example:      "1m",
				RequiredWith: "session_rate_limit_requests",
				Meta:         map[string]any{"format": "go_duration"},
			},
			{
				Key:          "app_key_rate_limit_requests",
				Type:         "integer",
				Title:        "Per app key request limit",
				Description:  "Maximum requests for a single application API key during the paired window. Set 0 to disable.",
				DefaultValue: defaults.AppKeyRateLimitRequests,
				Example:      120,
				Minimum:      &zero,
				RequiredWith: "app_key_rate_limit_window",
			},
			{
				Key:          "app_key_rate_limit_window",
				Type:         "duration",
				Title:        "Per app key request window",
				Description:  "Duration string for per-app-key limiting.",
				DefaultValue: defaults.AppKeyRateLimitWindow.String(),
				Example:      "1m",
				RequiredWith: "app_key_rate_limit_requests",
				Meta:         map[string]any{"format": "go_duration"},
			},
			{
				Key:          "model_key_rate_limit_requests",
				Type:         "integer",
				Title:        "Per model key request limit",
				Description:  "Maximum requests for a single virtual model API key during the paired window. Set 0 to disable.",
				DefaultValue: defaults.ModelKeyRateLimitRequests,
				Example:      120,
				Minimum:      &zero,
				RequiredWith: "model_key_rate_limit_window",
			},
			{
				Key:          "model_key_rate_limit_window",
				Type:         "duration",
				Title:        "Per model key request window",
				Description:  "Duration string for per-model-key limiting.",
				DefaultValue: defaults.ModelKeyRateLimitWindow.String(),
				Example:      "1m",
				RequiredWith: "model_key_rate_limit_requests",
				Meta:         map[string]any{"format": "go_duration"},
			},
		},
	}
}

func (s *SettingsService) Set(ctx context.Context, input map[string]any) (map[string]any, error) {
	if s == nil || s.store == nil {
		return nil, ErrInvalidAccessSettings
	}
	current, err := s.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := current
	if next.GlobalRateLimitRequests, err = intFromAny(input["global_rate_limit_requests"], current.GlobalRateLimitRequests); err != nil {
		return nil, err
	}
	if next.GlobalRateLimitWindow, err = durationFromAny(input["global_rate_limit_window"], current.GlobalRateLimitWindow); err != nil {
		return nil, err
	}
	if next.UserRateLimitRequests, err = intFromAny(input["user_rate_limit_requests"], current.UserRateLimitRequests); err != nil {
		return nil, err
	}
	if next.UserRateLimitWindow, err = durationFromAny(input["user_rate_limit_window"], current.UserRateLimitWindow); err != nil {
		return nil, err
	}
	if next.SessionRateLimitRequests, err = intFromAny(input["session_rate_limit_requests"], current.SessionRateLimitRequests); err != nil {
		return nil, err
	}
	if next.SessionRateLimitWindow, err = durationFromAny(input["session_rate_limit_window"], current.SessionRateLimitWindow); err != nil {
		return nil, err
	}
	if next.AppKeyRateLimitRequests, err = intFromAny(input["app_key_rate_limit_requests"], current.AppKeyRateLimitRequests); err != nil {
		return nil, err
	}
	if next.AppKeyRateLimitWindow, err = durationFromAny(input["app_key_rate_limit_window"], current.AppKeyRateLimitWindow); err != nil {
		return nil, err
	}
	if next.ModelKeyRateLimitRequests, err = intFromAny(input["model_key_rate_limit_requests"], current.ModelKeyRateLimitRequests); err != nil {
		return nil, err
	}
	if next.ModelKeyRateLimitWindow, err = durationFromAny(input["model_key_rate_limit_window"], current.ModelKeyRateLimitWindow); err != nil {
		return nil, err
	}
	if err := validateSettings(next); err != nil {
		return nil, err
	}
	if _, err := s.store.SetSystemConfig(ctx, store.SetSystemConfigInput{
		Key:   systemAccessSettingsKey,
		Value: settingsMap(next),
	}); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cachedValue = next
	s.cachedAt = time.Now().UTC()
	s.mu.Unlock()
	return settingsMap(next), nil
}

func (s *SettingsService) load(ctx context.Context) (Settings, error) {
	value := s.defaults
	item, err := s.store.GetSystemConfig(ctx, systemAccessSettingsKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			if err := validateSettings(value); err != nil {
				return Settings{}, err
			}
			return value, nil
		}
		return Settings{}, err
	}
	var parseErr error
	if value.GlobalRateLimitRequests, parseErr = intFromAny(item.Value["global_rate_limit_requests"], value.GlobalRateLimitRequests); parseErr != nil {
		return Settings{}, parseErr
	}
	if value.GlobalRateLimitWindow, parseErr = durationFromAny(item.Value["global_rate_limit_window"], value.GlobalRateLimitWindow); parseErr != nil {
		return Settings{}, parseErr
	}
	if value.UserRateLimitRequests, parseErr = intFromAny(item.Value["user_rate_limit_requests"], value.UserRateLimitRequests); parseErr != nil {
		return Settings{}, parseErr
	}
	if value.UserRateLimitWindow, parseErr = durationFromAny(item.Value["user_rate_limit_window"], value.UserRateLimitWindow); parseErr != nil {
		return Settings{}, parseErr
	}
	if value.SessionRateLimitRequests, parseErr = intFromAny(item.Value["session_rate_limit_requests"], value.SessionRateLimitRequests); parseErr != nil {
		return Settings{}, parseErr
	}
	if value.SessionRateLimitWindow, parseErr = durationFromAny(item.Value["session_rate_limit_window"], value.SessionRateLimitWindow); parseErr != nil {
		return Settings{}, parseErr
	}
	if value.AppKeyRateLimitRequests, parseErr = intFromAny(item.Value["app_key_rate_limit_requests"], value.AppKeyRateLimitRequests); parseErr != nil {
		return Settings{}, parseErr
	}
	if value.AppKeyRateLimitWindow, parseErr = durationFromAny(item.Value["app_key_rate_limit_window"], value.AppKeyRateLimitWindow); parseErr != nil {
		return Settings{}, parseErr
	}
	if value.ModelKeyRateLimitRequests, parseErr = intFromAny(item.Value["model_key_rate_limit_requests"], value.ModelKeyRateLimitRequests); parseErr != nil {
		return Settings{}, parseErr
	}
	if value.ModelKeyRateLimitWindow, parseErr = durationFromAny(item.Value["model_key_rate_limit_window"], value.ModelKeyRateLimitWindow); parseErr != nil {
		return Settings{}, parseErr
	}
	if err := validateSettings(value); err != nil {
		return Settings{}, err
	}
	return value, nil
}

func settingsMap(value Settings) map[string]any {
	return map[string]any{
		"global_rate_limit_requests":    value.GlobalRateLimitRequests,
		"global_rate_limit_window":      value.GlobalRateLimitWindow.String(),
		"user_rate_limit_requests":      value.UserRateLimitRequests,
		"user_rate_limit_window":        value.UserRateLimitWindow.String(),
		"session_rate_limit_requests":   value.SessionRateLimitRequests,
		"session_rate_limit_window":     value.SessionRateLimitWindow.String(),
		"app_key_rate_limit_requests":   value.AppKeyRateLimitRequests,
		"app_key_rate_limit_window":     value.AppKeyRateLimitWindow.String(),
		"model_key_rate_limit_requests": value.ModelKeyRateLimitRequests,
		"model_key_rate_limit_window":   value.ModelKeyRateLimitWindow.String(),
	}
}

func validateSettings(value Settings) error {
	checks := []struct {
		name   string
		max    int
		window time.Duration
	}{
		{"global", value.GlobalRateLimitRequests, value.GlobalRateLimitWindow},
		{"user", value.UserRateLimitRequests, value.UserRateLimitWindow},
		{"session", value.SessionRateLimitRequests, value.SessionRateLimitWindow},
		{"app_key", value.AppKeyRateLimitRequests, value.AppKeyRateLimitWindow},
		{"model_key", value.ModelKeyRateLimitRequests, value.ModelKeyRateLimitWindow},
	}
	for _, check := range checks {
		if check.max < 0 {
			return fmt.Errorf("%s rate limit requests must be non-negative", check.name)
		}
		if check.max > 0 && check.window <= 0 {
			return fmt.Errorf("%s rate limit window must be positive", check.name)
		}
	}
	return nil
}

func intFromAny(value any, fallback int) (int, error) {
	switch raw := value.(type) {
	case nil:
		return fallback, nil
	case int:
		return raw, nil
	case int64:
		return int(raw), nil
	case float64:
		return int(raw), nil
	case string:
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return fallback, nil
		}
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid integer value %q", raw)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("invalid integer value type %T", value)
	}
}

func durationFromAny(value any, fallback time.Duration) (time.Duration, error) {
	switch raw := value.(type) {
	case nil:
		return fallback, nil
	case string:
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return fallback, nil
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid duration value %q", raw)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("invalid duration value type %T", value)
	}
}
