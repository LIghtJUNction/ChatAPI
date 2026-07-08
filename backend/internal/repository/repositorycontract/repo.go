package repositorycontract

import (
	"github.com/zyf/chatapi/internal/repository/auditrepo"
	"github.com/zyf/chatapi/internal/repository/authrepo"
	"github.com/zyf/chatapi/internal/repository/chatrepo"
	"github.com/zyf/chatapi/internal/repository/configrepo"
	"github.com/zyf/chatapi/internal/repository/platformrepo"
	"github.com/zyf/chatapi/internal/repository/storagerepo"
)

type Store interface {
	platformrepo.MaintenanceStore
	authrepo.Store
	configrepo.Store
	storagerepo.Store
	auditrepo.Store
	chatrepo.Store
}
