package audit

import (
	"context"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type Store interface {
	CreateAuditLog(context.Context, common.CreateAuditLogInput) (common.AuditLog, error)
	ListAuditLogs(context.Context, common.ListAuditLogsInput) ([]common.AuditLog, error)
	CountAuditLogs(context.Context, common.CountAuditLogsInput) (int, error)
	CreateAppAPIKeyAuditLog(context.Context, common.AppAPIKeyAuditLog) error
	ListAppAPIKeyAuditLogs(context.Context, common.ListAppAPIKeyAuditLogsInput) ([]common.AppAPIKeyAuditLog, error)
}
