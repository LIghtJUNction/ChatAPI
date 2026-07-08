package model

import (
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

type Service struct {
	store     store.Store
	masterKey string
	Logger    *zap.Logger
}

const lastUsedMinInterval = 5 * time.Minute

func NewService(dataStore store.Store, masterKey string) *Service {
	return &Service{store: dataStore, masterKey: strings.TrimSpace(masterKey)}
}
