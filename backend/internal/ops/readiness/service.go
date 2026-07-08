package readiness

import (
	"context"
	"time"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/repository/platformrepo"
	"github.com/zyf/chatapi/internal/store"
)

type Service struct {
	cfg   config.Config
	store platformrepo.HealthStore
}

type Report struct {
	OK          bool           `json:"ok"`
	Mode        config.Mode    `json:"mode"`
	GeneratedAt time.Time      `json:"generated_at"`
	Database    Check          `json:"database"`
	Migration   MigrationCheck `json:"migration"`
}

type Check struct {
	OK     bool   `json:"ok"`
	Driver string `json:"driver,omitempty"`
	Error  string `json:"error,omitempty"`
}

type MigrationCheck struct {
	OK             bool                     `json:"ok"`
	SchemaVersion  string                   `json:"schema_version,omitempty"`
	MigrationDirty bool                     `json:"migration_dirty"`
	MigrationLock  string                   `json:"migration_lock,omitempty"`
	LastMigratedAt string                   `json:"last_migrated_at,omitempty"`
	Applied        []store.AppliedMigration `json:"applied,omitempty"`
	Error          string                   `json:"error,omitempty"`
}

func NewService(cfg config.Config, dataStore platformrepo.HealthStore) *Service {
	return &Service{cfg: cfg, store: dataStore}
}

func (s *Service) Check(ctx context.Context) Report {
	report := Report{
		OK:          true,
		Mode:        s.cfg.Mode,
		GeneratedAt: time.Now().UTC(),
		Database: Check{
			OK:     true,
			Driver: s.cfg.DatabaseDriver,
		},
		Migration: MigrationCheck{OK: true},
	}
	if s == nil || s.store == nil {
		report.OK = false
		report.Database.OK = false
		report.Database.Error = "store unavailable"
		return report
	}
	if err := s.store.Ping(ctx); err != nil {
		report.OK = false
		report.Database.OK = false
		report.Database.Error = err.Error()
	}
	status, err := s.store.MigrationStatus(ctx)
	if err != nil {
		report.OK = false
		report.Migration.OK = false
		report.Migration.Error = err.Error()
		return report
	}
	report.Migration.SchemaVersion = status.SchemaVersion
	report.Migration.MigrationDirty = status.MigrationDirty
	report.Migration.MigrationLock = status.MigrationLock
	report.Migration.LastMigratedAt = status.LastMigratedAt
	report.Migration.Applied = status.Applied
	if status.MigrationDirty {
		report.OK = false
		report.Migration.OK = false
		report.Migration.Error = "migration dirty"
	}
	return report
}
