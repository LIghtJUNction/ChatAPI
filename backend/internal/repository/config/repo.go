package config

import (
	"context"

	"github.com/zyf/chatapi/internal/repository/common"
)

type Store interface {
	GetSystemConfig(context.Context, string) (common.SystemConfig, error)
	SetSystemConfig(context.Context, common.SetSystemConfigInput) (common.SystemConfig, error)
	DeleteSystemConfig(context.Context, string) error
	ListSystemConfigs(context.Context) ([]common.SystemConfig, error)
	GetUserConfig(context.Context, string, string) (common.UserConfig, error)
	SetUserConfig(context.Context, common.SetUserConfigInput) (common.UserConfig, error)
	DeleteUserConfig(context.Context, string, string) error
	ListUserConfigs(context.Context, string) ([]common.UserConfig, error)
	ListAutomationRulesByUser(context.Context, string) ([]common.AutomationRule, error)
	ReplaceAutomationRulesForUser(context.Context, string, map[string]struct{}, []common.UpsertAutomationRuleInput) ([]common.AutomationRule, error)
}
