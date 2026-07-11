package settings

import (
	"context"
	"fmt"
	"time"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

type Settings struct{ PendingTurnTTL time.Duration }
type Service struct{ core *settingscore.Service }

func New(store settingscore.Store, cfg config.Config) *Service {
	min := float64(0)
	environment := cfg.SettingsEnvironment("chat")
	core := settingscore.New(store, settingscore.Spec{Domain: "chat", Title: "聊天与协议", StorageKey: "settings.chat", Defaults: map[string]any{"pending_turn_ttl": cfg.PendingTurnTTL.String()}, Environment: environment, Fields: []settingscore.Descriptor{
		{Key: "pending_turn_ttl", Type: "duration", Title: "等待请求有效期", Description: "超过该时间仍未完成的请求会被标记为过期；0s 表示禁用。", Level: settingscore.LevelPolicy, Editable: true, Default: cfg.PendingTurnTTL.String(), Minimum: &min, Unit: "duration"},
	}, Validate: func(v map[string]any) error {
		d, err := time.ParseDuration(settingscore.String(v["pending_turn_ttl"]))
		if err != nil || d < 0 {
			return fmt.Errorf("pending_turn_ttl must be a non-negative duration")
		}
		return nil
	}})
	return &Service{core: core}
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
	d, err := s.core.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	ttl, err := time.ParseDuration(settingscore.String(d.Values["pending_turn_ttl"]))
	return Settings{PendingTurnTTL: ttl}, err
}
