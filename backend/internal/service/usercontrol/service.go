package usercontrol

import (
	"context"

	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	configrepo "github.com/zyf2007/ChatAPI/internal/repository/config"
	"github.com/zyf2007/ChatAPI/internal/repository/storage"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	identitysvc "github.com/zyf2007/ChatAPI/internal/service/auth/authn/identity"
	localauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/local"
	authsettings "github.com/zyf2007/ChatAPI/internal/service/auth/authn/settings"
	totpsvc "github.com/zyf2007/ChatAPI/internal/service/auth/authn/totp"
	appkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	automationsvc "github.com/zyf2007/ChatAPI/internal/service/automation"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	workspacesettings "github.com/zyf2007/ChatAPI/internal/service/chat/workspace/settings"
	userconfig "github.com/zyf2007/ChatAPI/internal/service/usercontrol/config"
	"github.com/zyf2007/ChatAPI/internal/service/usercontrol/conversations"
	"github.com/zyf2007/ChatAPI/internal/service/usercontrol/identity"
	"github.com/zyf2007/ChatAPI/internal/service/usercontrol/keys"
	"github.com/zyf2007/ChatAPI/internal/service/usercontrol/profile"
	"go.uber.org/zap"
)

type Service struct {
	Profile       *profile.Service
	Keys          *keys.Service
	Config        *userconfig.Service
	Identity      *identity.Service
	Conversations *conversations.Service
	Automation    *automationsvc.Service
}

type Deps struct {
	Identity          *identitysvc.Service
	LocalAuth         *localauth.Service
	Settings          *authsettings.Service
	TOTP              *totpsvc.Service
	Policy            *policy.Service
	Query             *turnquerysvc.Service
	Turn              *controlsvc.Service
	Configs           configrepo.Store
	Storage           storage.Store
	Chat              chat.Store
	AppKeysStore      auth.KeyStore
	AppKeys           *appkey.Service
	ModelKeys         *modelkey.Service
	Accounts          *account.Service
	Logger            *zap.Logger
	Events            chatevents.Publisher
	Automation        *automationsvc.Service
	RealtimeSettings  *workspacesettings.Service
	ConversationLimit func(context.Context) int
}

func New(deps Deps) *Service {
	profileDeps := profile.Deps{
		Identity:          deps.Identity,
		LocalAuth:         deps.LocalAuth,
		Settings:          deps.Settings,
		TOTP:              deps.TOTP,
		Policy:            deps.Policy,
		Logger:            deps.Logger,
		Realtime:          deps.RealtimeSettings,
		ConversationLimit: deps.ConversationLimit,
	}
	if deps.Query != nil {
		profileDeps.Conversations = deps.Query
	}
	return &Service{
		Profile:  profile.New(profileDeps),
		Keys:     keys.New(keys.Deps{Keys: deps.AppKeysStore, AppKeys: deps.AppKeys, ModelKeys: deps.ModelKeys, Logger: deps.Logger}),
		Config:   userconfig.New(userconfig.Deps{Configs: deps.Configs, Chat: deps.Chat, Events: deps.Events, Logger: deps.Logger}),
		Identity: identity.New(identity.Deps{Accounts: deps.Accounts, Logger: deps.Logger}),
		Conversations: conversations.New(conversations.Deps{
			Query:  deps.Query,
			Turn:   deps.Turn,
			Logger: deps.Logger,
			Events: deps.Events,
			DeleteOne: func(ctx context.Context, conversationID string) (common.DeleteConversationsResult, error) {
				return deps.Chat.DeleteConversations(ctx, []string{conversationID})
			},
			DeleteMany: deps.Chat.DeleteConversations,
		}),
		Automation: deps.Automation,
	}
}
