package admincontrol

import (
	"github.com/zyf/chatapi/internal/repository/authrepo"
	"github.com/zyf/chatapi/internal/repository/chatrepo"
	"github.com/zyf/chatapi/internal/repository/storagerepo"
	"github.com/zyf/chatapi/internal/service/account"
	authaccess "github.com/zyf/chatapi/internal/service/auth/access"
	authsettings "github.com/zyf/chatapi/internal/service/auth/authn/settings"
	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
	turnquerysvc "github.com/zyf/chatapi/internal/service/chat/turnquery"
)

type Deps struct {
	Accounts       *account.Service
	Query          *turnquerysvc.Service
	Turn           *turnsvc.Service
	ChatStore      chatrepo.Store
	StorageStore   storagerepo.Store
	KeyStore       authrepo.KeyStore
	AuthSettings   *authsettings.Service
	AccessSettings *authaccess.SettingsService
}

type Service struct {
	accounts       *account.Service
	query          *turnquerysvc.Service
	turn           *turnsvc.Service
	chatStore      chatrepo.Store
	storageStore   storagerepo.Store
	keyStore       authrepo.KeyStore
	authSettings   *authsettings.Service
	accessSettings *authaccess.SettingsService
}

type CreateUserInput struct {
	Username   string
	Email      string
	Password   string
	Role       string
	IsActive   bool
	LocalAdmin bool
}

func New(deps Deps) *Service {
	return &Service{
		accounts:       deps.Accounts,
		query:          deps.Query,
		turn:           deps.Turn,
		chatStore:      deps.ChatStore,
		storageStore:   deps.StorageStore,
		keyStore:       deps.KeyStore,
		authSettings:   deps.AuthSettings,
		accessSettings: deps.AccessSettings,
	}
}
