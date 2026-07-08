package model

import (
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/repository/auth"
	"go.uber.org/zap"
)

type Service struct {
	store     auth.ModelKeyStore
	masterKey string
	Logger    *zap.Logger
}

const lastUsedMinInterval = 5 * time.Minute

func NewService(dataStore auth.ModelKeyStore, masterKey string) *Service {
	return &Service{store: dataStore, masterKey: strings.TrimSpace(masterKey)}
}
