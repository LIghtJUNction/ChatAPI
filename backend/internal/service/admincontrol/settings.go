package admincontrol

import "context"

import (
	adminsettings "github.com/zyf2007/ChatAPI/internal/service/admincontrol/settings"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

func (s *Service) SettingsCatalog() (map[string]any, error) {
	if s.settings == nil {
		return nil, errNotConfigured("admin settings")
	}
	return s.settings.Catalog(), nil
}
func (s *Service) SettingsOverview(ctx context.Context) (map[string]any, error) {
	if s.settings == nil {
		return nil, errNotConfigured("admin settings")
	}
	return s.settings.Overview(ctx)
}
func (s *Service) GetSettings(ctx context.Context, domain string) (settingscore.Document, error) {
	if s.settings == nil {
		return settingscore.Document{}, errNotConfigured("admin settings")
	}
	return s.settings.Get(ctx, domain)
}
func (s *Service) PatchSettings(ctx context.Context, domain string, input adminsettings.PatchInput) (adminsettings.PatchResult, error) {
	if s.settings == nil {
		return adminsettings.PatchResult{}, errNotConfigured("admin settings")
	}
	return s.settings.Patch(ctx, domain, input)
}
func (s *Service) RuntimeSettings() (map[string]any, error) {
	if s.settings == nil {
		return nil, errNotConfigured("admin settings")
	}
	return s.settings.Runtime(), nil
}
