package service

import (
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

func databaseInfoFromStore(cfg config.Config, dataStore store.Store) DatabaseInfo {
	info := DatabaseInfo{Driver: cfg.DatabaseDriver}
	if strings.EqualFold(strings.TrimSpace(cfg.DatabaseDriver), "sqlite") {
		info.SQLitePath = cfg.DatabaseDSN
		if stat, err := os.Stat(cfg.DatabaseDSN); err == nil {
			info.SQLiteBytes = stat.Size()
		}
		walPath := cfg.DatabaseDSN + "-wal"
		info.SQLiteWAL = walPath
		if stat, err := os.Stat(walPath); err == nil {
			info.SQLiteWALBytes = stat.Size()
		}
		return info
	}
	poolProvider, ok := dataStore.(interface{ Pool() *pgxpool.Pool })
	if !ok || poolProvider.Pool() == nil {
		return info
	}
	stats := poolProvider.Pool().Stat()
	info.PostgresMaxConns = stats.MaxConns()
	info.PostgresTotalConns = stats.TotalConns()
	info.PostgresAcquiredConns = stats.AcquiredConns()
	info.PostgresIdleConns = stats.IdleConns()
	info.PostgresConstructingConns = stats.ConstructingConns()
	info.PostgresEmptyAcquireCount = stats.EmptyAcquireCount()
	info.PostgresCanceledAcquireCount = stats.CanceledAcquireCount()
	return info
}
