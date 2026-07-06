package service

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

var ErrInvalidSystemConfig = errors.New("invalid system config")

type SystemConfigService struct {
	store store.Store
}

func NewSystemConfigService(dataStore store.Store) *SystemConfigService {
	return &SystemConfigService{store: dataStore}
}

func (s *SystemConfigService) List(ctx context.Context) ([]store.SystemConfig, map[string]any, error) {
	if s == nil || s.store == nil {
		return nil, nil, ErrInvalidSystemConfig
	}
	items, err := s.store.ListSystemConfigs(ctx)
	if err != nil {
		return nil, nil, err
	}
	return items, aggregateSystemConfigs(items), nil
}

func (s *SystemConfigService) SetMany(ctx context.Context, values map[string]any) ([]store.SystemConfig, map[string]any, error) {
	if s == nil || s.store == nil {
		return nil, nil, ErrInvalidSystemConfig
	}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, nil, ErrInvalidSystemConfig
		}
		valueMap, ok := value.(map[string]any)
		if !ok {
			return nil, nil, ErrInvalidSystemConfig
		}
		if _, err := s.store.SetSystemConfig(ctx, store.SetSystemConfigInput{
			Key:   key,
			Value: valueMap,
		}); err != nil {
			return nil, nil, err
		}
	}
	return s.List(ctx)
}

func aggregateSystemConfigs(items []store.SystemConfig) map[string]any {
	out := make(map[string]any, len(items))
	for _, item := range items {
		out[item.Key] = item.Value
	}
	return out
}
