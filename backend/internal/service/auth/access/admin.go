package access

import (
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

func (s *SettingsService) AdminDomain() *settingscore.Service {
	return s.core
}
func (s *SettingsService) newAdminDomain() *settingscore.Service {
	zero := float64(0)
	d := s.defaults
	fields := []settingscore.Descriptor{{Key: "user_conversation_limit", Type: "integer", Title: "用户会话数上限", Description: "每个用户保留的最新会话数，达到上限后自动删除较早的会话，0 表示不限制。", Level: settingscore.LevelCommon, Editable: true, Default: d.UserConversationLimit, Minimum: &zero}}
	add := func(key, title string, requests int, window string) {
		fields = append(fields, settingscore.Descriptor{Key: key + "_rate_limit_requests", Type: "integer", Title: title + "请求上限", Description: "窗口内允许的最大请求数，0 表示禁用。", Level: settingscore.LevelCommon, Editable: true, Default: requests, Minimum: &zero}, settingscore.Descriptor{Key: key + "_rate_limit_window", Type: "duration", Title: title + "限流窗口", Description: "限流统计窗口，例如 1m。", Level: settingscore.LevelPolicy, Editable: true, Default: window, Unit: "duration"})
	}
	add("global", "匿名访问", d.GlobalRateLimitRequests, d.GlobalRateLimitWindow.String())
	add("user", "用户", d.UserRateLimitRequests, d.UserRateLimitWindow.String())
	add("session", "会话", d.SessionRateLimitRequests, d.SessionRateLimitWindow.String())
	add("app_key", "应用 Key", d.AppKeyRateLimitRequests, d.AppKeyRateLimitWindow.String())
	add("model_key", "模型 Key", d.ModelKeyRateLimitRequests, d.ModelKeyRateLimitWindow.String())
	return settingscore.New(s.store, settingscore.Spec{Domain: "access", Title: "访问限流", StorageKey: systemAccessSettingsKey, Defaults: settingsMap(d), Environment: s.environment, Fields: fields, Validate: func(v map[string]any) error {
		next, err := settingsFromMap(v, d)
		if err != nil {
			return err
		}
		return validateSettings(next)
	}})
}
func settingsFromMap(v map[string]any, d Settings) (Settings, error) {
	var e error
	n := d
	if n.UserConversationLimit, e = intFromAny(v["user_conversation_limit"], d.UserConversationLimit); e != nil {
		return Settings{}, e
	}
	if n.GlobalRateLimitRequests, e = intFromAny(v["global_rate_limit_requests"], d.GlobalRateLimitRequests); e != nil {
		return Settings{}, e
	}
	if n.GlobalRateLimitWindow, e = durationFromAny(v["global_rate_limit_window"], d.GlobalRateLimitWindow); e != nil {
		return Settings{}, e
	}
	if n.UserRateLimitRequests, e = intFromAny(v["user_rate_limit_requests"], d.UserRateLimitRequests); e != nil {
		return Settings{}, e
	}
	if n.UserRateLimitWindow, e = durationFromAny(v["user_rate_limit_window"], d.UserRateLimitWindow); e != nil {
		return Settings{}, e
	}
	if n.SessionRateLimitRequests, e = intFromAny(v["session_rate_limit_requests"], d.SessionRateLimitRequests); e != nil {
		return Settings{}, e
	}
	if n.SessionRateLimitWindow, e = durationFromAny(v["session_rate_limit_window"], d.SessionRateLimitWindow); e != nil {
		return Settings{}, e
	}
	if n.AppKeyRateLimitRequests, e = intFromAny(v["app_key_rate_limit_requests"], d.AppKeyRateLimitRequests); e != nil {
		return Settings{}, e
	}
	if n.AppKeyRateLimitWindow, e = durationFromAny(v["app_key_rate_limit_window"], d.AppKeyRateLimitWindow); e != nil {
		return Settings{}, e
	}
	if n.ModelKeyRateLimitRequests, e = intFromAny(v["model_key_rate_limit_requests"], d.ModelKeyRateLimitRequests); e != nil {
		return Settings{}, e
	}
	if n.ModelKeyRateLimitWindow, e = durationFromAny(v["model_key_rate_limit_window"], d.ModelKeyRateLimitWindow); e != nil {
		return Settings{}, e
	}
	return n, nil
}
