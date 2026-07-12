package platform

import (
	"context"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
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
