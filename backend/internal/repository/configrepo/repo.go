package configrepo

import (
	"context"

	"github.com/zyf/chatapi/internal/store"
)

type Store interface {
	GetSystemConfig(context.Context, string) (store.SystemConfig, error)
	SetSystemConfig(context.Context, store.SetSystemConfigInput) (store.SystemConfig, error)
	DeleteSystemConfig(context.Context, string) error
	ListSystemConfigs(context.Context) ([]store.SystemConfig, error)
	GetUserConfig(context.Context, string, string) (store.UserConfig, error)
	SetUserConfig(context.Context, store.SetUserConfigInput) (store.UserConfig, error)
	DeleteUserConfig(context.Context, string, string) error
	ListUserConfigs(context.Context, string) ([]store.UserConfig, error)
	ListAutomationRulesByUser(context.Context, string) ([]store.AutomationRule, error)
	ReplaceAutomationRulesForUser(context.Context, string, map[string]struct{}, []store.UpsertAutomationRuleInput) ([]store.AutomationRule, error)
}
