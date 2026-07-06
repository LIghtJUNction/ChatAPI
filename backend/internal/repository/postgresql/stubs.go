package postgresql

import (
	"context"

	"github.com/zyf/chatapi/internal/store"
)

func (s *Store) MigrationStatus(context.Context) (store.MigrationStatus, error) {
	return store.MigrationStatus{}, errNotImplemented
}

func (s *Store) Checkpoint(context.Context) error { return nil }

func (s *Store) Vacuum(context.Context) error { return nil }
