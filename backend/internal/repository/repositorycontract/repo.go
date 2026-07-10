package repositorycontract

import (
	"github.com/zyf2007/ChatAPI/internal/repository/audit"
	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/repository/automation"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/config"
	"github.com/zyf2007/ChatAPI/internal/repository/platform"
	"github.com/zyf2007/ChatAPI/internal/repository/storage"
)

type Store interface {
	platform.MaintenanceStore
	auth.Store
	automation.Store
	config.Store
	storage.Store
	audit.Store
	chat.Store
}
