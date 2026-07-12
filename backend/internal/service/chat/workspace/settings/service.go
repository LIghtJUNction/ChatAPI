package settings

import (
	"context"
	"fmt"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

type Settings struct {
	MaxConnections        int
	MaxConnectionsPerUser int
}
type Service struct{ core *settingscore.Service }

func New(store settingscore.Store, cfg config.Config) *Service {
	zero := float64(0)
	environment := cfg.SettingsEnvironment("realtime")
	validate := func(values map[string]any) error {
		for _, key := range []string{"max_connections_per_instance", "max_connections_per_user_per_instance"} {
			value, ok := settingscore.Number(values[key])
			if !ok || value < 0 {
				return fmt.Errorf("%s must be non-negative", key)
			}
		}
		return nil
	}
	return &Service{core: settingscore.New(store, settingscore.Spec{Domain: "realtime", Title: "实时通信", StorageKey: "settings.realtime", Defaults: map[string]any{"max_connections_per_instance": cfg.RealtimeMaxConnections, "max_connections_per_user_per_instance": cfg.RealtimeMaxConnectionsPerUser}, Environment: environment, Validate: validate, Fields: []settingscore.Descriptor{
		{Key: "max_connections_per_instance", Type: "integer", Title: "单实例连接上限", Description: "当前服务实例上的工作台 WebSocket 总连接上限，0 表示不限制。", Level: settingscore.LevelCommon, Editable: true, Default: cfg.RealtimeMaxConnections, Minimum: &zero},
		{Key: "max_connections_per_user_per_instance", Type: "integer", Title: "单用户单实例上限", Description: "每个用户在当前服务实例上可同时建立的工作台连接数。", Level: settingscore.LevelCommon, Editable: true, Default: cfg.RealtimeMaxConnectionsPerUser, Minimum: &zero},
	}})}
}
func (s *Service) Domain() string                                         { return s.core.Domain() }
func (s *Service) Title() string                                          { return s.core.Title() }
func (s *Service) Fields() []settingscore.Descriptor                      { return s.core.Fields() }
func (s *Service) Get(ctx context.Context) (settingscore.Document, error) { return s.core.Get(ctx) }
func (s *Service) Reload(ctx context.Context) (settingscore.Document, error) {
	return s.core.Reload(ctx)
}
func (s *Service) Patch(ctx context.Context, v map[string]any) (settingscore.Document, []string, error) {
	return s.core.Patch(ctx, v)
}
func (s *Service) Current(ctx context.Context) (Settings, error) {
	doc, err := s.core.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	number := func(key string) int { value, _ := settingscore.Number(doc.Values[key]); return int(value) }
	return Settings{MaxConnections: number("max_connections_per_instance"), MaxConnectionsPerUser: number("max_connections_per_user_per_instance")}, nil
}
