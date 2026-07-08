package app

import (
	"sync"
	"time"

	"github.com/zyf/chatapi/internal/repository/authrepo"
	"go.uber.org/zap"
)

type Service struct {
	store         authrepo.AppKeyStore
	rateLimitMu   sync.Mutex
	rateLimitHits map[string][]time.Time
	Logger        *zap.Logger
}

const appLastUsedMinInterval = 5 * time.Minute

func NewService(dataStore authrepo.AppKeyStore) *Service {
	return &Service{store: dataStore, rateLimitHits: map[string][]time.Time{}}
}
