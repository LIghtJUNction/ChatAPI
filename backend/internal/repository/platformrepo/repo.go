package platformrepo

import (
	"context"

	"github.com/zyf/chatapi/internal/store"
)

type HealthStore interface {
	Ping(context.Context) error
	MigrationStatus(context.Context) (store.MigrationStatus, error)
}

type MaintenanceStore interface {
	HealthStore
	Checkpoint(context.Context) error
	Vacuum(context.Context) error
}
