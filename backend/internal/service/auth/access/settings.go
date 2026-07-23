package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

const systemAccessSettingsKey = "system_access_settings"

var ErrInvalidAccessSettings = errors.New("invalid access settings")

type Settings struct {
	UserConversationLimit     int           `json:"user_conversation_limit"`
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
	store       auth.SettingsStore
	defaults    Settings
	core        *settingscore.Service
	environment map[string]any
}

func NewSettingsService(dataStore auth.SettingsStore, defaults Settings, environment ...map[string]any) *SettingsService {
	s := &SettingsService{store: dataStore, defaults: defaults}
	if len(environment) > 0 {
		s.environment = environment[0]
	}
	s.core = s.newAdminDomain()
	return s
}

func (s *SettingsService) Get(ctx context.Context) (Settings, error) {
	if s == nil || s.store == nil {
		return Settings{}, ErrInvalidAccessSettings
	}
	doc, err := s.core.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	return settingsFromMap(doc.Values, s.defaults)
}

func settingsMap(value Settings) map[string]any {
	return map[string]any{
		"user_conversation_limit":       value.UserConversationLimit,
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
	if value.UserConversationLimit < 0 {
		return fmt.Errorf("user conversation limit must be non-negative")
	}
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
	case json.Number:
		parsed, err := strconv.ParseInt(raw.String(), 10, 64)
		if err != nil || int64(int(parsed)) != parsed {
			return 0, fmt.Errorf("invalid integer value %q", raw.String())
		}
		return int(parsed), nil
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
