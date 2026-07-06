package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/store"
)

func TestOIDCAuthAutoCreatesVerifiedAdminByEmail(t *testing.T) {
	st := newOIDCTestStore(t)
	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.OIDCEnabled = true
	cfg.OIDCAutoCreateUser = true
	cfg.OIDCAdminEmails = []string{"admin@example.com"}
	svc := NewOIDCAuthService(st, cfg)

	actor, err := svc.Authenticate(context.Background(), OIDCClaims{
		Subject:       "sub-admin",
		Email:         "admin@example.com",
		EmailVerified: true,
		Name:          "Admin User",
		Profile: map[string]any{
			"sub":   "sub-admin",
			"email": "admin@example.com",
		},
	})
	if err != nil {
		t.Fatalf("authenticate oidc admin: %v", err)
	}
	if actor.Role != "admin" || actor.Source != "oidc" {
		t.Fatalf("unexpected admin actor: %#v", actor)
	}
	user, err := st.GetUser(context.Background(), actor.UserID)
	if err != nil {
		t.Fatalf("get oidc user: %v", err)
	}
	if user.Email != "admin@example.com" || user.Role != "admin" || user.LocalAdmin || user.LastLoginAt == nil {
		t.Fatalf("unexpected created admin user: %#v", user)
	}
	identity, err := st.GetUserIdentity(context.Background(), "oidc", "sub-admin")
	if err != nil {
		t.Fatalf("get oidc identity: %v", err)
	}
	if identity.UserID != user.ID || !identity.EmailVerified {
		t.Fatalf("unexpected oidc identity: %#v", identity)
	}
}

func TestOIDCAuthRejectsUnverifiedAdminEmail(t *testing.T) {
	st := newOIDCTestStore(t)
	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.OIDCEnabled = true
	cfg.OIDCAutoCreateUser = true
	cfg.OIDCAdminEmails = []string{"admin@example.com"}
	svc := NewOIDCAuthService(st, cfg)

	_, err := svc.Authenticate(context.Background(), OIDCClaims{
		Subject:       "sub-user",
		Email:         "admin@example.com",
		EmailVerified: false,
	})
	if !errors.Is(err, ErrOIDCEmailUnverified) {
		t.Fatalf("expected unverified email rejection, got %v", err)
	}
}

func TestOIDCAuthDemotesWhenAdminEmailListChanges(t *testing.T) {
	st := newOIDCTestStore(t)
	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.OIDCEnabled = true
	cfg.OIDCAutoCreateUser = true
	cfg.OIDCAdminEmails = []string{"admin@example.com"}
	svc := NewOIDCAuthService(st, cfg)

	actor, err := svc.Authenticate(context.Background(), OIDCClaims{
		Subject:       "sub-admin",
		Email:         "admin@example.com",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("authenticate oidc admin: %v", err)
	}
	if actor.Role != "admin" {
		t.Fatalf("expected admin role, got %#v", actor)
	}

	cfg.OIDCAdminEmails = nil
	svc = NewOIDCAuthService(st, cfg)
	actor, err = svc.Authenticate(context.Background(), OIDCClaims{
		Subject:       "sub-admin",
		Email:         "admin@example.com",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("authenticate after admin list change: %v", err)
	}
	if actor.Role != "user" {
		t.Fatalf("expected OIDC admin to be demoted after list change, got %#v", actor)
	}
	user, err := st.GetUser(context.Background(), actor.UserID)
	if err != nil {
		t.Fatalf("get demoted user: %v", err)
	}
	if user.LocalAdmin {
		t.Fatalf("OIDC admin promotion should not set local_admin: %#v", user)
	}
}

func TestOIDCAuthRejectsDisallowedEmail(t *testing.T) {
	st := newOIDCTestStore(t)
	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.OIDCEnabled = true
	cfg.OIDCAutoCreateUser = true
	cfg.OIDCAllowedDomains = []string{"example.com"}
	svc := NewOIDCAuthService(st, cfg)

	_, err := svc.Authenticate(context.Background(), OIDCClaims{
		Subject:       "sub-denied",
		Email:         "denied@other.test",
		EmailVerified: true,
	})
	if !errors.Is(err, ErrOIDCAccessDenied) {
		t.Fatalf("expected access denied for outside domain, got %v", err)
	}
}

func TestOIDCAuthRequiresExistingUserWhenAutoCreateDisabled(t *testing.T) {
	st := newOIDCTestStore(t)
	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.OIDCEnabled = true
	cfg.OIDCAutoCreateUser = false
	svc := NewOIDCAuthService(st, cfg)

	_, err := svc.Authenticate(context.Background(), OIDCClaims{
		Subject:       "sub-missing",
		Email:         "missing@example.com",
		EmailVerified: true,
	})
	if !errors.Is(err, ErrOIDCUserNotFound) {
		t.Fatalf("expected missing user error, got %v", err)
	}

	user, err := st.CreateUser(context.Background(), store.CreateUserInput{
		ID:       "user_existing",
		Username: "existing",
		Email:    "existing@example.com",
		Role:     "user",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create existing user: %v", err)
	}
	actor, err := svc.Authenticate(context.Background(), OIDCClaims{
		Subject:       "sub-existing",
		Email:         "existing@example.com",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("authenticate existing user by email: %v", err)
	}
	if actor.UserID != user.ID {
		t.Fatalf("expected linked existing user, got %#v", actor)
	}
}

func TestOIDCAuthRequiresVerifiedEmailToLinkExistingUser(t *testing.T) {
	st := newOIDCTestStore(t)
	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.OIDCEnabled = true
	cfg.OIDCAutoCreateUser = false
	svc := NewOIDCAuthService(st, cfg)

	if _, err := st.CreateUser(context.Background(), store.CreateUserInput{
		ID:       "user_existing",
		Username: "existing",
		Email:    "existing@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create existing user: %v", err)
	}
	_, err := svc.Authenticate(context.Background(), OIDCClaims{
		Subject:       "sub-existing",
		Email:         "existing@example.com",
		EmailVerified: false,
	})
	if !errors.Is(err, ErrOIDCEmailUnverified) {
		t.Fatalf("expected unverified email rejection, got %v", err)
	}
}

func TestOIDCAuthBindIdentityLinksCurrentUser(t *testing.T) {
	st := newOIDCTestStore(t)
	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.OIDCEnabled = true
	svc := NewOIDCAuthService(st, cfg)

	user, err := st.CreateUser(context.Background(), store.CreateUserInput{
		ID:       "user_bind",
		Username: "bind-user",
		Email:    "",
		Role:     "user",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create bind user: %v", err)
	}

	updated, identity, err := svc.BindIdentity(context.Background(), user.ID, OIDCClaims{
		Subject:       "sub-bind",
		Email:         "bind@example.com",
		EmailVerified: true,
		Name:          "Bind User",
		Profile:       map[string]any{"sub": "sub-bind"},
	})
	if err != nil {
		t.Fatalf("bind oidc identity: %v", err)
	}
	if updated.Email != "bind@example.com" || updated.LastLoginAt == nil {
		t.Fatalf("unexpected updated user after bind: %#v", updated)
	}
	if identity.UserID != user.ID || identity.Subject != "sub-bind" || !identity.EmailVerified {
		t.Fatalf("unexpected identity after bind: %#v", identity)
	}
}

func TestOIDCAuthBindIdentityRejectsForeignLink(t *testing.T) {
	st := newOIDCTestStore(t)
	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.OIDCEnabled = true
	svc := NewOIDCAuthService(st, cfg)

	firstUser, err := st.CreateUser(context.Background(), store.CreateUserInput{
		ID:       "user_first",
		Username: "first-user",
		Email:    "first@example.com",
		Role:     "user",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create first user: %v", err)
	}
	secondUser, err := st.CreateUser(context.Background(), store.CreateUserInput{
		ID:       "user_second",
		Username: "second-user",
		Email:    "second@example.com",
		Role:     "user",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}
	if _, _, err := svc.BindIdentity(context.Background(), firstUser.ID, OIDCClaims{
		Subject:       "sub-shared",
		Email:         "first@example.com",
		EmailVerified: true,
	}); err != nil {
		t.Fatalf("seed first bind: %v", err)
	}
	if _, _, err := svc.BindIdentity(context.Background(), secondUser.ID, OIDCClaims{
		Subject:       "sub-shared",
		Email:         "second@example.com",
		EmailVerified: true,
	}); !errors.Is(err, ErrOIDCIdentityConflict) {
		t.Fatalf("expected foreign identity conflict, got %v", err)
	}
}

func newOIDCTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "chatapi.sqlite3")
	st, err := sqlitestore.Open(dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite: %v", err)
	}
	return st
}
