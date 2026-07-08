package repositorycontract

import (
	"github.com/zyf/chatapi/internal/repository/audit"
	"github.com/zyf/chatapi/internal/repository/auth"
	"github.com/zyf/chatapi/internal/repository/chat"
	"github.com/zyf/chatapi/internal/repository/config"
	"github.com/zyf/chatapi/internal/repository/platform"
	"github.com/zyf/chatapi/internal/repository/storage"
)

type Store interface {
	platform.MaintenanceStore
	auth.Store
	config.Store
	storage.Store
	audit.Store
	chat.Store
}
