package admin

import (
	"github.com/zyf/chatapi/internal/service/account"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	"github.com/zyf/chatapi/internal/store"
)

type Service struct {
	accounts *account.Service
	store    store.Store
	policies *policy.Service
}

type CreateUserInput struct {
	Username   string
	Email      string
	Password   string
	Role       string
	IsActive   bool
	LocalAdmin bool
}

func NewService(accounts *account.Service, dataStore store.Store, policies *policy.Service) *Service {
	return &Service{accounts: accounts, store: dataStore, policies: policies}
}
