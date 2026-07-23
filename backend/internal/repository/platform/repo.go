package platform

import (
	"context"
	"errors"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

var ErrMaintenanceUnsupported = errors.New("database maintenance operation is unsupported")

type HealthStore interface {
	Ping(context.Context) error
	MigrationStatus(context.Context) (common.MigrationStatus, error)
}

type MaintenanceStore interface {
	HealthStore
	Checkpoint(context.Context) error
	Vacuum(context.Context) error
}
