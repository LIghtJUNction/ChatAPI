package settings

import (
	"context"
	"fmt"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

type Settings struct {
	Enabled           bool
	MaxSteps          int
	MaxLoopIntervalMS int64
}
type Service struct{ core *settingscore.Service }

func New(store settingscore.Store) *Service {
	zero, one := float64(0), float64(1)
	validate := func(values map[string]any) error {
		steps, ok := settingscore.Number(values["max_steps_per_rule"])
		if !ok || steps < 1 {
			return fmt.Errorf("max_steps_per_rule must be positive")
		}
		loop, ok := settingscore.Number(values["max_loop_interval_ms"])
		if !ok || loop < 0 {
			return fmt.Errorf("max_loop_interval_ms must be non-negative")
		}
		return nil
	}
	return &Service{core: settingscore.New(store, settingscore.Spec{Domain: "automation", Title: "自动化", StorageKey: "settings.automation", Defaults: map[string]any{"enabled": true, "max_steps_per_rule": 100, "max_loop_interval_ms": 3600000}, Validate: validate, Fields: []settingscore.Descriptor{
		{Key: "enabled", Type: "boolean", Title: "允许自动化执行", Description: "关闭后保留规则，但不再匹配和播放。", Level: settingscore.LevelCommon, Editable: true, Default: true},
		{Key: "max_steps_per_rule", Type: "integer", Title: "单规则步骤上限", Description: "限制录制规则的最大步骤数。", Level: settingscore.LevelPolicy, Editable: true, Default: 100, Minimum: &one},
		{Key: "max_loop_interval_ms", Type: "integer", Title: "最大循环间隔", Description: "自动化循环间隔允许的最大毫秒数。", Level: settingscore.LevelAdvanced, Editable: true, Default: 3600000, Minimum: &zero, Unit: "ms"},
	}})}
}
func (s *Service) Domain() string                                         { return s.core.Domain() }
func (s *Service) Title() string                                          { return s.core.Title() }
func (s *Service) Fields() []settingscore.Descriptor                      { return s.core.Fields() }
func (s *Service) Get(ctx context.Context) (settingscore.Document, error) { return s.core.Get(ctx) }
func (s *Service) Reload(ctx context.Context) (settingscore.Document, error) {
	return s.core.Reload(ctx)
}
func (s *Service) ValidatePatch(ctx context.Context, v map[string]any) error {
	return s.core.ValidatePatch(ctx, v)
}
func (s *Service) PreparePatch(ctx context.Context, v map[string]any) (settingscore.PreparedPatch, error) {
	return s.core.PreparePatch(ctx, v)
}
func (s *Service) Patch(ctx context.Context, v map[string]any) (settingscore.Document, []string, error) {
	return s.core.Patch(ctx, v)
}
func (s *Service) Current(ctx context.Context) (Settings, error) {
	doc, err := s.core.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	steps, _ := settingscore.Number(doc.Values["max_steps_per_rule"])
	loop, _ := settingscore.Number(doc.Values["max_loop_interval_ms"])
	return Settings{Enabled: settingscore.Bool(doc.Values["enabled"]), MaxSteps: int(steps), MaxLoopIntervalMS: int64(loop)}, nil
}
