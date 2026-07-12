package settings

import (
	"context"
	"fmt"
	"time"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

type Settings struct {
	PendingTurnTTL            time.Duration
	MaxOutputEventsPerMessage int
}
type Service struct{ core *settingscore.Service }

func New(store settingscore.Store, cfg config.Config) *Service {
	min := float64(0)
	environment := cfg.SettingsEnvironment("chat")
	core := settingscore.New(store, settingscore.Spec{Domain: "chat", Title: "聊天与协议", StorageKey: "settings.chat", Defaults: map[string]any{"pending_turn_ttl": cfg.PendingTurnTTL.String(), "max_output_events_per_message": 0}, Environment: environment, Fields: []settingscore.Descriptor{
		{Key: "pending_turn_ttl", Type: "duration", Title: "等待请求有效期", Description: "超过该时间仍未完成的请求会被标记为过期；0s 表示禁用。", Level: settingscore.LevelPolicy, Editable: true, Default: cfg.PendingTurnTTL.String(), Minimum: &min, Unit: "duration"},
		{Key: "max_output_events_per_message", Type: "integer", Title: "单条消息最大事件数", Description: "单条消息允许接受的工作台输出事件数；一次流式输出、完成输出或内置工具操作各算一个事件。超限后按原请求协议中止，0 表示不限制。", Level: settingscore.LevelCommon, Editable: true, Default: 0, Minimum: &min},
	}, Validate: func(v map[string]any) error {
		d, err := time.ParseDuration(settingscore.String(v["pending_turn_ttl"]))
		if err != nil || d < 0 {
			return fmt.Errorf("pending_turn_ttl must be a non-negative duration")
		}
		maxEvents, ok := settingscore.Number(v["max_output_events_per_message"])
		if !ok || maxEvents < 0 {
			return fmt.Errorf("max_output_events_per_message must be non-negative")
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
	maxEvents, _ := settingscore.Number(d.Values["max_output_events_per_message"])
	return Settings{PendingTurnTTL: ttl, MaxOutputEventsPerMessage: int(maxEvents)}, err
}
