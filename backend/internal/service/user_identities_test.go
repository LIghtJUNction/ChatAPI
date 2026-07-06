package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/store"
)

func TestUserIdentityServiceProtectsLastLoginMethod(t *testing.T) {
	st := newUserIdentityTestStore(t)
	if _, err := st.CreateUser(context.Background(), store.CreateUserInput{
		ID:       "user_oidc_only",
		Email:    "oidc@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	identity, err := st.UpsertUserIdentity(context.Background(), store.UpsertUserIdentityInput{
		ID:       "identity_only",
		UserID:   "user_oidc_only",
		Provider: "oidc",
		Subject:  "sub-only",
		Email:    "oidc@example.com",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	svc := NewUserIdentityService(st)

	err = svc.Unlink(context.Background(), "user_oidc_only", identity.ID)
	if !errors.Is(err, ErrLastLoginMethod) {
		t.Fatalf("expected last login method protection, got %v", err)
	}
}

func TestUserIdentityServiceAllowsLocalPasswordUserToUnlink(t *testing.T) {
	st := newUserIdentityTestStore(t)
	if _, err := st.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_local",
		Username:     "local",
		Email:        "local@example.com",
		PasswordHash: "hash",
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	identity, err := st.UpsertUserIdentity(context.Background(), store.UpsertUserIdentityInput{
		ID:       "identity_local",
		UserID:   "user_local",
		Provider: "oidc",
		Subject:  "sub-local",
		Email:    "local@example.com",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	svc := NewUserIdentityService(st)

	if err := svc.Unlink(context.Background(), "user_local", identity.ID); err != nil {
		t.Fatalf("unlink local password user identity: %v", err)
	}
	if _, err := st.GetUserIdentity(context.Background(), "oidc", "sub-local"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected identity deleted, got %v", err)
	}
}

func newUserIdentityTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite: %v", err)
	}
	return st
}
