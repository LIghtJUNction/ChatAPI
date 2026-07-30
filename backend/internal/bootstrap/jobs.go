package bootstrap

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	platformrepo "github.com/zyf2007/ChatAPI/internal/repository/platform"
	chatsettings "github.com/zyf2007/ChatAPI/internal/service/chat/settings"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

func expirePendingLoop(ctx context.Context, turns *turnsvc.Service, settings *chatsettings.Service, logger *zap.Logger) {
	if turns == nil || settings == nil {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			current, err := settings.Current(ctx)
			if err != nil {
				logger.Warn("load chat settings for pending expiry failed", zap.Error(err))
				continue
			}
			if current.PendingTurnTTL <= 0 {
				continue
			}
			if _, err := turns.ExpirePendingTurns(ctx, current.PendingTurnTTL, now); err != nil {
				logger.Warn("expire pending turns failed", zap.Error(err))
			}
		}
	}
}

type maintenanceAuditRecorder interface {
	Record(context.Context, common.CreateAuditLogInput) (common.AuditLog, error)
}

func storageVacuumLoop(ctx context.Context, cfg config.Config, store platformrepo.MaintenanceStore, audit maintenanceAuditRecorder, logger *zap.Logger) {
	if !StorageVacuumEnabled(cfg) || store == nil {
		return
	}
	hour, minute, err := config.ParseDailyTime(cfg.StorageCleanupTime)
	if err != nil {
		logger.Warn("storage vacuum scheduler disabled by invalid cleanup time", zap.Error(err))
		return
	}
	for {
		next := NextDailyRun(time.Now(), hour, minute)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		startedAt := time.Now()
		vacuumCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		err := store.Vacuum(vacuumCtx)
		cancel()
		metadata := map[string]any{
			"scheduled_time": cfg.StorageCleanupTime,
			"duration_ms":    time.Since(startedAt).Milliseconds(),
		}
		if err != nil {
			logger.Warn("scheduled storage vacuum failed", zap.Error(err))
			metadata["error"] = err.Error()
			recordMaintenanceAudit(ctx, audit, "failure", metadata, logger)
			continue
		}
		logger.Info("scheduled storage vacuum completed")
		recordMaintenanceAudit(ctx, audit, "success", metadata, logger)
	}
}

func StorageVacuumEnabled(cfg config.Config) bool {
	return cfg.StorageCleanupEnabled && cfg.StorageVacuumEnabled && strings.EqualFold(strings.TrimSpace(cfg.DatabaseDriver), "sqlite")
}

func recordMaintenanceAudit(ctx context.Context, audit maintenanceAuditRecorder, outcome string, metadata map[string]any, logger *zap.Logger) {
	if audit == nil {
		return
	}
	if _, err := audit.Record(ctx, common.CreateAuditLogInput{
		EventType: "system.storage.vacuum", ResourceType: "database", ResourceID: "primary",
		Action: "vacuum", Outcome: outcome, Metadata: metadata,
	}); err != nil {
		logger.Warn("record storage vacuum audit failed", zap.Error(err))
	}
}

func NextDailyRun(now time.Time, hour int, minute int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
