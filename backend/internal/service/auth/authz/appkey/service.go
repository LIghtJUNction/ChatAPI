package app

import (
	"strings"
	"sync"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"go.uber.org/zap"
)

type Service struct {
	store         auth.AppKeyStore
	masterKey     string
	rateLimitMu   sync.Mutex
	rateLimitHits map[string][]time.Time
	Logger        *zap.Logger
}

const appLastUsedMinInterval = 5 * time.Minute

func NewService(dataStore auth.AppKeyStore, masterKey ...string) *Service {
	key := ""
	if len(masterKey) > 0 {
		key = strings.TrimSpace(masterKey[0])
	}
	return &Service{store: dataStore, masterKey: key, rateLimitHits: map[string][]time.Time{}}
}
