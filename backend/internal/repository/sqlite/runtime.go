package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"go.uber.org/zap"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/repository/common"
	"github.com/zyf/chatapi/internal/repository/migrations"
)

type Store struct {
	db     *sql.DB
	Logger *zap.Logger
}

var errNotFound = common.ErrNotFound
var errConflict = common.ErrTurnConflict

func Open(dsn string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) logger(ctx context.Context) *zap.Logger {
	return logging.BindContext(s.Logger, ctx)
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) MigrationStatus(ctx context.Context) (common.MigrationStatus, error) {
	status, err := migrations.StatusReport(ctx, s.db)
	if err != nil {
		return common.MigrationStatus{}, err
	}
	applied := make([]common.AppliedMigration, 0, len(status.Applied))
	for _, item := range status.Applied {
		applied = append(applied, common.AppliedMigration{
			Version:   item.Version,
			Name:      item.Name,
			AppliedAt: item.AppliedAt,
			Checksum:  item.Checksum,
			Dirty:     item.Dirty,
		})
	}
	return common.MigrationStatus{
		SchemaVersion:  status.SchemaVersion,
		AppVersion:     status.AppVersion,
		MigrationDirty: status.MigrationDirty,
		MigrationLock:  status.MigrationLock,
		CreatedBy:      status.CreatedBy,
		LastMigratedAt: status.LastMigratedAt,
		Applied:        applied,
	}, nil
}

func (s *Store) Checkpoint(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE);`); err != nil {
		return fmt.Errorf("sqlite wal checkpoint: %w", err)
	}
	return nil
}

func (s *Store) Vacuum(ctx context.Context) error {
	if err := s.Checkpoint(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM;`); err != nil {
		return fmt.Errorf("sqlite vacuum: %w", err)
	}
	return nil
}
