package usercontrol

import (
	"context"

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
	"github.com/zyf/chatapi/internal/service/usercontrol/config"
	"github.com/zyf/chatapi/internal/service/usercontrol/conversations"
	"github.com/zyf/chatapi/internal/service/usercontrol/identity"
	"github.com/zyf/chatapi/internal/service/usercontrol/keys"
	"github.com/zyf/chatapi/internal/service/usercontrol/profile"
	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

type Service struct {
	Profile       *profile.Service
	Keys          *keys.Service
	Config        *config.Service
	Identity      *identity.Service
	Conversations *conversations.Service
}

type Deps struct {
	Identity  *identitysvc.Service
	LocalAuth *localauth.Service
	Settings  *authsettings.Service
	TOTP      *totpsvc.Service
	Policy    *policy.Service
	Query     *turnquerysvc.Service
	Turn      *turnsvc.Service
	Store     store.Store
	AppKeys   *appkey.Service
	ModelKeys *modelkey.Service
	Accounts  *account.Service
	Logger    *zap.Logger
}

func New(deps Deps) *Service {
	return &Service{
		Profile:  profile.New(profile.Deps{Identity: deps.Identity, LocalAuth: deps.LocalAuth, Settings: deps.Settings, TOTP: deps.TOTP, Policy: deps.Policy, Logger: deps.Logger}),
		Keys:     keys.New(keys.Deps{Store: deps.Store, AppKeys: deps.AppKeys, ModelKeys: deps.ModelKeys, Logger: deps.Logger}),
		Config:   config.New(config.Deps{Store: deps.Store, Logger: deps.Logger}),
		Identity: identity.New(identity.Deps{Accounts: deps.Accounts, Logger: deps.Logger}),
		Conversations: conversations.New(conversations.Deps{
			Query:  deps.Query,
			Turn:   deps.Turn,
			Logger: deps.Logger,
			DeleteOne: func(ctx context.Context, conversationID string) (store.DeleteConversationsResult, error) {
				return deps.Store.DeleteConversations(ctx, []string{conversationID})
			},
			DeleteMany: deps.Store.DeleteConversations,
		}),
	}
}
