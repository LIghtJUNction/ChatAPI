package platform

import (
	"context"

	"github.com/zyf/chatapi/internal/repository/common"
)

type HealthStore interface {
	Ping(context.Context) error
	MigrationStatus(context.Context) (common.MigrationStatus, error)
}

type MaintenanceStore interface {
	HealthStore
	Checkpoint(context.Context) error
	Vacuum(context.Context) error
}
