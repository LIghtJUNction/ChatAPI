package settings

import (
	"context"
	"fmt"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

type Settings struct {
	Enabled, AllowRemoteURL, AllowDataURL, AllowBase64, AllowSVG bool
	MaxBytes                                                     int64
	MaxImages, AVIFQuality                                       int
}
type Service struct{ core *settingscore.Service }

func New(store settingscore.Store, cfg config.Config) *Service {
	zero, maxQuality := float64(0), float64(100)
	env := cfg.SettingsEnvironment("media")
	defaults := map[string]any{"enabled": cfg.MediaProcessEnabled, "allow_remote_url": cfg.MediaAllowRemoteURL, "allow_data_url": cfg.MediaAllowDataURL, "allow_base64": cfg.MediaAllowBase64, "allow_svg": cfg.MediaAllowSVG, "max_bytes": cfg.MediaMaxBytes, "max_images_per_request": cfg.MediaMaxImagesPerRequest, "avif_quality": cfg.MediaAVIFQuality}
	fields := []settingscore.Descriptor{
		{Key: "enabled", Type: "boolean", Title: "图片预处理", Description: "解析并统一转码请求中的图片。", Level: settingscore.LevelCommon, Editable: true, Default: cfg.MediaProcessEnabled},
		{Key: "max_bytes", Type: "integer", Title: "单张图片上限", Description: "原始图片允许的最大字节数，0 表示不限制。", Level: settingscore.LevelCommon, Editable: true, Default: cfg.MediaMaxBytes, Minimum: &zero, Unit: "bytes"},
		{Key: "max_images_per_request", Type: "integer", Title: "单请求图片数量", Description: "每个请求最多包含的图片数，0 表示不限制。", Level: settingscore.LevelPolicy, Editable: true, Default: cfg.MediaMaxImagesPerRequest, Minimum: &zero},
		{Key: "allow_remote_url", Type: "boolean", Title: "允许远程图片", Description: "允许协议请求引用远程 URL。", Level: settingscore.LevelPolicy, Editable: true, Default: cfg.MediaAllowRemoteURL},
		{Key: "allow_data_url", Type: "boolean", Title: "允许 Data URL", Description: "允许 data:image/... 输入。", Level: settingscore.LevelPolicy, Editable: true, Default: cfg.MediaAllowDataURL},
		{Key: "allow_base64", Type: "boolean", Title: "允许裸 Base64", Description: "允许不带 Data URL 前缀的 Base64 图片。", Level: settingscore.LevelAdvanced, Editable: true, Default: cfg.MediaAllowBase64},
		{Key: "allow_svg", Type: "boolean", Title: "允许 SVG", Description: "SVG 可能包含主动内容，默认拒绝。", Level: settingscore.LevelAdvanced, Editable: true, Default: cfg.MediaAllowSVG},
		{Key: "avif_quality", Type: "integer", Title: "AVIF 质量", Description: "落盘 AVIF 的编码质量。", Level: settingscore.LevelAdvanced, Editable: true, Default: cfg.MediaAVIFQuality, Minimum: &zero, Maximum: &maxQuality},
	}
	validate := func(v map[string]any) error {
		for _, k := range []string{"max_bytes", "max_images_per_request"} {
			n, ok := settingscore.Number(v[k])
			if !ok || n < 0 {
				return fmt.Errorf("%s must be non-negative", k)
			}
		}
		q, ok := settingscore.Number(v["avif_quality"])
		if !ok || q < 0 || q > 100 {
			return fmt.Errorf("avif_quality must be between 0 and 100")
		}
		return nil
	}
	return &Service{core: settingscore.New(store, settingscore.Spec{Domain: "media", Title: "媒体", StorageKey: "settings.media", Defaults: defaults, Environment: env, Fields: fields, Validate: validate})}
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
func (s *Service) Patch(ctx context.Context, v map[string]any) (settingscore.Document, []string, error) {
	return s.core.Patch(ctx, v)
}
func (s *Service) Current(ctx context.Context) (Settings, error) {
	d, e := s.core.Get(ctx)
	if e != nil {
		return Settings{}, e
	}
	num := func(k string) int64 { n, _ := settingscore.Number(d.Values[k]); return int64(n) }
	return Settings{Enabled: settingscore.Bool(d.Values["enabled"]), AllowRemoteURL: settingscore.Bool(d.Values["allow_remote_url"]), AllowDataURL: settingscore.Bool(d.Values["allow_data_url"]), AllowBase64: settingscore.Bool(d.Values["allow_base64"]), AllowSVG: settingscore.Bool(d.Values["allow_svg"]), MaxBytes: num("max_bytes"), MaxImages: int(num("max_images_per_request")), AVIFQuality: int(num("avif_quality"))}, nil
}
