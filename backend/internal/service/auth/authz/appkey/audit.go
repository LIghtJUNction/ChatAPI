package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"go.uber.org/zap"
)

func (s *Service) RecordAudit(ctx context.Context, principal Principal, route string, statusCode int, errorCode string) {
	item := common.AppAPIKeyAuditLog{
		ID:          "applog_" + uuid.NewString(),
		AppAPIKeyID: principal.KeyID,
		UserID:      principal.UserID,
		Route:       strings.TrimSpace(route),
		StatusCode:  statusCode,
		ErrorCode:   strings.TrimSpace(errorCode),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.CreateAppAPIKeyAuditLog(ctx, item); err != nil {
		logging.BindContext(s.Logger, ctx,
			zap.String("audit.kind", "app_api_key"),
			zap.String("app_api_key.id", principal.KeyID),
			zap.String("route", item.Route),
			zap.Int("http.status_code", statusCode),
			zap.String("error.code", item.ErrorCode),
		).Warn("failed to write app api key audit log", zap.Error(err))
		return
	}
	logging.BindContext(s.Logger, ctx,
		zap.String("audit.kind", "app_api_key"),
		zap.String("app_api_key.id", principal.KeyID),
		zap.String("route", item.Route),
		zap.Int("http.status_code", statusCode),
		zap.String("error.code", item.ErrorCode),
	).Debug("wrote app api key audit log")
}
