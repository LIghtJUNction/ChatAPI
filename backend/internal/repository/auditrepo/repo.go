package auditrepo

import (
	"context"

	"github.com/zyf/chatapi/internal/store"
)

type Store interface {
	CreateAuditLog(context.Context, store.CreateAuditLogInput) (store.AuditLog, error)
	ListAuditLogs(context.Context, store.ListAuditLogsInput) ([]store.AuditLog, error)
	CountAuditLogs(context.Context, store.CountAuditLogsInput) (int, error)
	CreateAppAPIKeyAuditLog(context.Context, store.AppAPIKeyAuditLog) error
	ListAppAPIKeyAuditLogs(context.Context, store.ListAppAPIKeyAuditLogsInput) ([]store.AppAPIKeyAuditLog, error)
}
