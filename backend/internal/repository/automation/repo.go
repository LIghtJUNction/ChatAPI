package automation

import (
	"context"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

// Store persists one automation rule aggregate at a time. Rule payload semantics belong to the
// automation service; repository implementations only preserve the JSON document.
type Store interface {
	ListAutomationRulesByUser(context.Context, string) ([]common.AutomationRule, error)
	UpsertAutomationRule(context.Context, common.UpsertAutomationRuleInput) (common.AutomationRule, error)
	DeleteAutomationRule(context.Context, string, string) error
}
