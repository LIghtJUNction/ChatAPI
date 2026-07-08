package postgresql

import (
	"context"

	"github.com/zyf/chatapi/internal/repository/common"
)

func (s *Store) MigrationStatus(ctx context.Context) (common.MigrationStatus, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM db_meta`)
	if err != nil {
		return common.MigrationStatus{}, err
	}
	defer rows.Close()

	meta := map[string]string{}
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return common.MigrationStatus{}, err
		}
		meta[key] = value
	}
	if err := rows.Err(); err != nil {
		return common.MigrationStatus{}, err
	}

	rows, err = s.pool.Query(ctx, `
		SELECT version, name, TO_CHAR(applied_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'), checksum, dirty
		FROM schema_migrations
		ORDER BY version ASC
	`)
	if err != nil {
		return common.MigrationStatus{}, err
	}
	defer rows.Close()

	applied := make([]common.AppliedMigration, 0)
	dirty := meta["migration_dirty"] == "1"
	for rows.Next() {
		var item common.AppliedMigration
		if err := rows.Scan(&item.Version, &item.Name, &item.AppliedAt, &item.Checksum, &item.Dirty); err != nil {
			return common.MigrationStatus{}, err
		}
		if item.Dirty {
			dirty = true
		}
		applied = append(applied, item)
	}
	if err := rows.Err(); err != nil {
		return common.MigrationStatus{}, err
	}

	return common.MigrationStatus{
		SchemaVersion:  meta["schema_version"],
		AppVersion:     meta["app_version"],
		MigrationDirty: dirty,
		MigrationLock:  meta["migration_lock"],
		CreatedBy:      meta["created_by"],
		LastMigratedAt: meta["last_migrated_at"],
		Applied:        applied,
	}, nil
}

func (s *Store) Checkpoint(context.Context) error { return nil }

func (s *Store) Vacuum(context.Context) error { return nil }
