package auth

import (
	"context"
	"time"

	"github.com/zyf/chatapi/internal/repository/common"
)

type UserStore interface {
	CreateUser(context.Context, common.CreateUserInput) (common.User, error)
	UpdateUser(context.Context, common.UpdateUserInput) (common.User, error)
	GetUser(context.Context, string) (common.User, error)
	GetUserByEmail(context.Context, string) (common.User, error)
	GetUserByUsername(context.Context, string) (common.User, error)
	ListUsers(context.Context) ([]common.User, error)
	PreviewUserDeletion(context.Context, string) (common.UserDeletionPreview, error)
	DeleteUserAccount(context.Context, string) error
	TransferUserOwnership(context.Context, string, string) (common.UserOwnershipTransferResult, error)
	TransferUserOwnershipSelection(context.Context, string, string, []string, []string) (common.UserOwnershipTransferResult, error)
}

type IdentityStore interface {
	UpsertUserIdentity(context.Context, common.UpsertUserIdentityInput) (common.UserIdentity, error)
	GetUserIdentity(context.Context, string, string) (common.UserIdentity, error)
	ListUserIdentities(context.Context, string) ([]common.UserIdentity, error)
	DeleteUserIdentity(context.Context, string, string) error
}

type AppKeyStore interface {
	CreateAppAPIKey(context.Context, common.CreateAppAPIKeyInput) (common.AppAPIKey, error)
	ListAppAPIKeysByUser(context.Context, string) ([]common.AppAPIKey, error)
	GetAppAPIKeyByPrefix(context.Context, string) (common.AppAPIKey, error)
	UpdateAppAPIKeyLastUsedAt(context.Context, string, time.Time) error
	RevokeAppAPIKey(context.Context, string, string) error
	CreateAppAPIKeyAuditLog(context.Context, common.AppAPIKeyAuditLog) error
	ListAppAPIKeyAuditLogs(context.Context, common.ListAppAPIKeyAuditLogsInput) ([]common.AppAPIKeyAuditLog, error)
}

type ModelKeyStore interface {
	CreateModelAPIKey(context.Context, common.CreateModelAPIKeyInput) (common.ModelAPIKey, error)
	ListModelAPIKeysByUser(context.Context, string) ([]common.ModelAPIKey, error)
	GetModelAPIKeyByPrefix(context.Context, string) (common.ModelAPIKey, error)
	GetModelAPIKeyByID(context.Context, string) (common.ModelAPIKey, error)
	UpdateModelAPIKeyLastUsedAt(context.Context, string, time.Time) error
	RevokeModelAPIKey(context.Context, string, string) error
}

type VerificationStore interface {
	GetAuthVerificationCode(context.Context, string, string) (common.AuthVerificationCode, error)
	UpsertAuthVerificationCode(context.Context, common.UpsertAuthVerificationCodeInput) (common.AuthVerificationCode, error)
	DeleteAuthVerificationCode(context.Context, string, string) error
	DeleteExpiredAuthVerificationCodes(context.Context, time.Time) (int, error)
}

type SettingsStore interface {
	GetSystemConfig(context.Context, string) (common.SystemConfig, error)
	SetSystemConfig(context.Context, common.SetSystemConfigInput) (common.SystemConfig, error)
	DeleteSystemConfig(context.Context, string) error
	GetUserConfig(context.Context, string, string) (common.UserConfig, error)
	SetUserConfig(context.Context, common.SetUserConfigInput) (common.UserConfig, error)
	DeleteUserConfig(context.Context, string, string) error
}

type Store interface {
	UserStore
	IdentityStore
	AppKeyStore
	ModelKeyStore
	VerificationStore
	SettingsStore
}

type KeyStore interface {
	AppKeyStore
	ModelKeyStore
}
