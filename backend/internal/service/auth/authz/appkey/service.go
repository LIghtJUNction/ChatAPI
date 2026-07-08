package app

import (
	"sync"
	"time"

	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

type Service struct {
	store         store.Store
	rateLimitMu   sync.Mutex
	rateLimitHits map[string][]time.Time
	Logger        *zap.Logger
}

const appLastUsedMinInterval = 5 * time.Minute

func NewService(dataStore store.Store) *Service {
	return &Service{store: dataStore, rateLimitHits: map[string][]time.Time{}}
}
