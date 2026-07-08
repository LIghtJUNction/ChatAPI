package authrepo

import (
	"context"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

type UserStore interface {
	CreateUser(context.Context, store.CreateUserInput) (store.User, error)
	UpdateUser(context.Context, store.UpdateUserInput) (store.User, error)
	GetUser(context.Context, string) (store.User, error)
	GetUserByEmail(context.Context, string) (store.User, error)
	GetUserByUsername(context.Context, string) (store.User, error)
	ListUsers(context.Context) ([]store.User, error)
	PreviewUserDeletion(context.Context, string) (store.UserDeletionPreview, error)
	DeleteUserAccount(context.Context, string) error
	TransferUserOwnership(context.Context, string, string) (store.UserOwnershipTransferResult, error)
	TransferUserOwnershipSelection(context.Context, string, string, []string, []string) (store.UserOwnershipTransferResult, error)
}

type IdentityStore interface {
	UpsertUserIdentity(context.Context, store.UpsertUserIdentityInput) (store.UserIdentity, error)
	GetUserIdentity(context.Context, string, string) (store.UserIdentity, error)
	ListUserIdentities(context.Context, string) ([]store.UserIdentity, error)
	DeleteUserIdentity(context.Context, string, string) error
}

type AppKeyStore interface {
	CreateAppAPIKey(context.Context, store.CreateAppAPIKeyInput) (store.AppAPIKey, error)
	ListAppAPIKeysByUser(context.Context, string) ([]store.AppAPIKey, error)
	GetAppAPIKeyByPrefix(context.Context, string) (store.AppAPIKey, error)
	UpdateAppAPIKeyLastUsedAt(context.Context, string, time.Time) error
	RevokeAppAPIKey(context.Context, string, string) error
	CreateAppAPIKeyAuditLog(context.Context, store.AppAPIKeyAuditLog) error
	ListAppAPIKeyAuditLogs(context.Context, store.ListAppAPIKeyAuditLogsInput) ([]store.AppAPIKeyAuditLog, error)
}

type ModelKeyStore interface {
	CreateModelAPIKey(context.Context, store.CreateModelAPIKeyInput) (store.ModelAPIKey, error)
	ListModelAPIKeysByUser(context.Context, string) ([]store.ModelAPIKey, error)
	GetModelAPIKeyByPrefix(context.Context, string) (store.ModelAPIKey, error)
	GetModelAPIKeyByID(context.Context, string) (store.ModelAPIKey, error)
	UpdateModelAPIKeyLastUsedAt(context.Context, string, time.Time) error
	RevokeModelAPIKey(context.Context, string, string) error
}

type VerificationStore interface {
	GetAuthVerificationCode(context.Context, string, string) (store.AuthVerificationCode, error)
	UpsertAuthVerificationCode(context.Context, store.UpsertAuthVerificationCodeInput) (store.AuthVerificationCode, error)
	DeleteAuthVerificationCode(context.Context, string, string) error
	DeleteExpiredAuthVerificationCodes(context.Context, time.Time) (int, error)
}

type SettingsStore interface {
	GetSystemConfig(context.Context, string) (store.SystemConfig, error)
	SetSystemConfig(context.Context, store.SetSystemConfigInput) (store.SystemConfig, error)
	DeleteSystemConfig(context.Context, string) error
	GetUserConfig(context.Context, string, string) (store.UserConfig, error)
	SetUserConfig(context.Context, store.SetUserConfigInput) (store.UserConfig, error)
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
