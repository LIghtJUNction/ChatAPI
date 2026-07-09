package admincontrol

import (
	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/storage"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
	authsettings "github.com/zyf2007/ChatAPI/internal/service/auth/authn/settings"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
)

type Deps struct {
	Accounts       *account.Service
	Query          *turnquerysvc.Service
	Turn           *turnsvc.Service
	Control        *controlsvc.Service
	ChatStore      chat.Store
	StorageStore   storage.Store
	KeyStore       auth.KeyStore
	AuthSettings   *authsettings.Service
	AccessSettings *authaccess.SettingsService
}

type Service struct {
	accounts       *account.Service
	query          *turnquerysvc.Service
	control        *controlsvc.Service
	chatStore      chat.Store
	storageStore   storage.Store
	keyStore       auth.KeyStore
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
	control := deps.Control
	if control == nil {
		control = controlsvc.New(deps.Query, deps.Turn, nil)
	}
	return &Service{
		accounts:       deps.Accounts,
		query:          deps.Query,
		control:        control,
		chatStore:      deps.ChatStore,
		storageStore:   deps.StorageStore,
		keyStore:       deps.KeyStore,
		authSettings:   deps.AuthSettings,
		accessSettings: deps.AccessSettings,
	}
}
