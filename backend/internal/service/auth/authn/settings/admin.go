package settings

import (
	"fmt"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

func (s *Service) AdminDomain() *settingscore.Service {
	return s.core
}

func (s *Service) newAdminDomain() *settingscore.Service {
	defaults := map[string]any{"local_password_login_enabled": true, "external_registration_enabled": false, "email_verification_enabled": false, "registration_email_domain_restriction_enabled": false, "registration_email_domains": "", "password_reset_enabled": s.cfg.SMTPEnabled, "geetest_login_enabled": false, "geetest_register_enabled": false, "geetest_password_reset_enabled": false}
	fields := []settingscore.Descriptor{
		{Key: "local_password_login_enabled", Type: "boolean", Title: "本地密码登录", Description: "允许用户使用用户名和密码登录。", Level: settingscore.LevelCommon, Editable: true, Default: true},
		{Key: "external_registration_enabled", Type: "boolean", Title: "开放注册", Description: "允许访客创建普通用户账号。", Level: settingscore.LevelCommon, Editable: true, Default: false},
		{Key: "email_verification_enabled", Type: "boolean", Title: "注册邮箱验证", Description: "注册时要求完成邮箱验证码。", Level: settingscore.LevelCommon, Editable: true, Default: false},
		{Key: "password_reset_enabled", Type: "boolean", Title: "密码找回", Description: "允许通过已配置的邮件服务找回密码。", Level: settingscore.LevelPolicy, Editable: s.cfg.SMTPEnabled, Default: s.cfg.SMTPEnabled},
		{Key: "registration_email_domain_restriction_enabled", Type: "boolean", Title: "限制注册邮箱域名", Description: "只允许指定邮箱域名注册。", Level: settingscore.LevelPolicy, Editable: true, Default: false},
		{Key: "registration_email_domains", Type: "string", Title: "允许的邮箱域名", Description: "多个域名使用英文逗号分隔。", Level: settingscore.LevelPolicy, Editable: true, Default: ""},
		{Key: "geetest_login_enabled", Type: "boolean", Title: "登录人机验证", Description: "本地登录启用 GeeTest。", Level: settingscore.LevelAdvanced, Editable: geetestConfigured(s.cfg), Default: false},
		{Key: "geetest_register_enabled", Type: "boolean", Title: "注册人机验证", Description: "注册启用 GeeTest。", Level: settingscore.LevelAdvanced, Editable: geetestConfigured(s.cfg), Default: false},
		{Key: "geetest_password_reset_enabled", Type: "boolean", Title: "找回密码人机验证", Description: "密码找回启用 GeeTest。", Level: settingscore.LevelAdvanced, Editable: geetestConfigured(s.cfg), Default: false},
	}
	validate := func(v map[string]any) error {
		if settingscore.Bool(v["registration_email_domain_restriction_enabled"]) && settingscore.String(v["registration_email_domains"]) == "" {
			return fmt.Errorf("registration email domains are required")
		}
		return nil
	}
	environment := map[string]any{}
	if !s.cfg.SMTPEnabled {
		environment["password_reset_enabled"] = false
	}
	if !geetestConfigured(s.cfg) {
		environment["geetest_login_enabled"] = false
		environment["geetest_register_enabled"] = false
		environment["geetest_password_reset_enabled"] = false
	}
	return settingscore.New(s.store, settingscore.Spec{Domain: "auth", Title: "访问与认证", StorageKey: systemSettingsKey, Defaults: defaults, Environment: environment, Fields: fields, Validate: validate})
}
