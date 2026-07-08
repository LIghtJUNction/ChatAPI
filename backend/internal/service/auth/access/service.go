package access

import (
	"github.com/zyf2007/ChatAPI/internal/config"
	labauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
)

func NewService(cfg config.Config, lab *labauth.Service, settings *SettingsService) *Service {
	if lab == nil {
		lab = labauth.NewService(cfg)
	}
	if settings == nil {
		settings = NewSettingsService(nil, Settings{
			GlobalRateLimitRequests: cfg.AccessRateLimitRequests,
			GlobalRateLimitWindow:   cfg.AccessRateLimitWindow,
		})
	}
	defaults := settings.defaults
	return &Service{
		cfg:              cfg,
		lab:              lab,
		settings:         settings,
		trustedOrigins:   buildTrustedOrigins(cfg),
		anonymousLimiter: newRequestLimiter(defaults.GlobalRateLimitRequests, defaults.GlobalRateLimitWindow),
		principalLimiter: newMultiLimiter(),
	}
}
