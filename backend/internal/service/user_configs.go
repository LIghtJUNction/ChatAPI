package service

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

var ErrInvalidUserConfig = errors.New("invalid user config")

const reservedUserConfigPrefix = "security."

type UserConfigService struct {
	store store.Store
}

func NewUserConfigService(dataStore store.Store) *UserConfigService {
	return &UserConfigService{store: dataStore}
}

func (s *UserConfigService) List(ctx context.Context, userID string) ([]store.UserConfig, map[string]any, error) {
	if s == nil || s.store == nil {
		return nil, nil, ErrInvalidUserConfig
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil, ErrInvalidUserConfig
	}
	items, err := s.store.ListUserConfigs(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	filtered := filterVisibleUserConfigs(items)
	return filtered, aggregateUserConfigs(filtered), nil
}

func (s *UserConfigService) SetMany(ctx context.Context, userID string, values map[string]any) ([]store.UserConfig, map[string]any, error) {
	if s == nil || s.store == nil {
		return nil, nil, ErrInvalidUserConfig
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil, ErrInvalidUserConfig
	}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, nil, ErrInvalidUserConfig
		}
		if isReservedUserConfigKey(key) {
			return nil, nil, ErrInvalidUserConfig
		}
		valueMap, ok := value.(map[string]any)
		if !ok {
			return nil, nil, ErrInvalidUserConfig
		}
		if _, err := s.store.SetUserConfig(ctx, store.SetUserConfigInput{
			UserID: userID,
			Key:    key,
			Value:  valueMap,
		}); err != nil {
			return nil, nil, err
		}
	}
	return s.List(ctx, userID)
}

func aggregateUserConfigs(items []store.UserConfig) map[string]any {
	out := make(map[string]any, len(items))
	for _, item := range items {
		out[item.Key] = item.Value
	}
	return out
}

func filterVisibleUserConfigs(items []store.UserConfig) []store.UserConfig {
	out := make([]store.UserConfig, 0, len(items))
	for _, item := range items {
		if isReservedUserConfigKey(item.Key) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func isReservedUserConfigKey(key string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), reservedUserConfigPrefix)
}
