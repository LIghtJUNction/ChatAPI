package usercontrol

import (
	"context"

	"github.com/zyf/chatapi/internal/repository/auth"
	"github.com/zyf/chatapi/internal/repository/chat"
	"github.com/zyf/chatapi/internal/repository/common"
	configrepo "github.com/zyf/chatapi/internal/repository/config"
	"github.com/zyf/chatapi/internal/repository/storage"
	"github.com/zyf/chatapi/internal/service/account"
	identitysvc "github.com/zyf/chatapi/internal/service/auth/authn/identity"
	localauth "github.com/zyf/chatapi/internal/service/auth/authn/local"
	authsettings "github.com/zyf/chatapi/internal/service/auth/authn/settings"
	totpsvc "github.com/zyf/chatapi/internal/service/auth/authn/totp"
	appkey "github.com/zyf/chatapi/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf/chatapi/internal/service/auth/authz/modelkey"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
	turnquerysvc "github.com/zyf/chatapi/internal/service/chat/turnquery"
	userconfig "github.com/zyf/chatapi/internal/service/usercontrol/config"
	"github.com/zyf/chatapi/internal/service/usercontrol/conversations"
	"github.com/zyf/chatapi/internal/service/usercontrol/identity"
	"github.com/zyf/chatapi/internal/service/usercontrol/keys"
	"github.com/zyf/chatapi/internal/service/usercontrol/profile"
	"go.uber.org/zap"
)

type Service struct {
	Profile       *profile.Service
	Keys          *keys.Service
	Config        *userconfig.Service
	Identity      *identity.Service
	Conversations *conversations.Service
}

type Deps struct {
	Identity     *identitysvc.Service
	LocalAuth    *localauth.Service
	Settings     *authsettings.Service
	TOTP         *totpsvc.Service
	Policy       *policy.Service
	Query        *turnquerysvc.Service
	Turn         *turnsvc.Service
	Configs      configrepo.Store
	Storage      storage.Store
	Chat         chat.Store
	AppKeysStore auth.KeyStore
	AppKeys      *appkey.Service
	ModelKeys    *modelkey.Service
	Accounts     *account.Service
	Logger       *zap.Logger
}

func New(deps Deps) *Service {
	return &Service{
		Profile:  profile.New(profile.Deps{Identity: deps.Identity, LocalAuth: deps.LocalAuth, Settings: deps.Settings, TOTP: deps.TOTP, Policy: deps.Policy, Logger: deps.Logger}),
		Keys:     keys.New(keys.Deps{Keys: deps.AppKeysStore, AppKeys: deps.AppKeys, ModelKeys: deps.ModelKeys, Logger: deps.Logger}),
		Config:   userconfig.New(userconfig.Deps{Configs: deps.Configs, Chat: deps.Chat, Logger: deps.Logger}),
		Identity: identity.New(identity.Deps{Accounts: deps.Accounts, Logger: deps.Logger}),
		Conversations: conversations.New(conversations.Deps{
			Query:  deps.Query,
			Turn:   deps.Turn,
			Logger: deps.Logger,
			DeleteOne: func(ctx context.Context, conversationID string) (common.DeleteConversationsResult, error) {
				return deps.Chat.DeleteConversations(ctx, []string{conversationID})
			},
			DeleteMany: deps.Chat.DeleteConversations,
		}),
	}
}
