package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zyf/chatapi/internal/repository/migrations"
	"github.com/zyf/chatapi/internal/store"
)

func TestUserRepositoryCreatesUpdatesAndListsUsers(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	alice, err := st.CreateUser(ctx, store.CreateUserInput{
		ID:           "user_alice",
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "hash-1",
		Role:         "admin",
		IsActive:     true,
		LocalAdmin:   true,
	})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if alice.Role != "admin" || !alice.IsActive || !alice.LocalAdmin || alice.CreatedAt.IsZero() {
		t.Fatalf("unexpected alice: %#v", alice)
	}

	bob, err := st.CreateUser(ctx, store.CreateUserInput{
		ID:           "user_bob",
		Username:     "bob",
		Email:        "bob@example.com",
		PasswordHash: "hash-2",
		IsActive:     true,
	})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if bob.Role != "user" {
		t.Fatalf("empty role should default to user: %#v", bob)
	}

	byEmail, err := st.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if byEmail.ID != alice.ID || byEmail.PasswordHash != "hash-1" {
		t.Fatalf("unexpected user by email: %#v", byEmail)
	}

	lastLogin := time.Date(2026, 7, 6, 1, 2, 3, 0, time.UTC)
	updated, err := st.UpdateUser(ctx, store.UpdateUserInput{
		ID:           alice.ID,
		Username:     "alice2",
		Email:        "alice2@example.com",
		PasswordHash: "hash-3",
		Role:         "user",
		IsActive:     false,
		LocalAdmin:   false,
		LastLoginAt:  &lastLogin,
	})
	if err != nil {
		t.Fatalf("update alice: %v", err)
	}
	if updated.Username != "alice2" || updated.Email != "alice2@example.com" || updated.IsActive || updated.LocalAdmin {
		t.Fatalf("unexpected updated alice: %#v", updated)
	}
	if updated.LastLoginAt == nil || !updated.LastLoginAt.Equal(lastLogin) {
		t.Fatalf("unexpected last login: %#v", updated.LastLoginAt)
	}

	items, err := st.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two users, got %#v", items)
	}

	if _, err := st.GetUser(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing user, got %v", err)
	}
}

func TestUserIdentityRepositoryUpsertsByProviderSubject(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	if _, err := st.CreateUser(ctx, store.CreateUserInput{
		ID:       "user_oidc",
		Email:    "first@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	lastLogin := time.Date(2026, 7, 6, 4, 5, 6, 0, time.UTC)
	identity, err := st.UpsertUserIdentity(ctx, store.UpsertUserIdentityInput{
		ID:            "identity_1",
		UserID:        "user_oidc",
		Provider:      "kirari",
		Subject:       "sub-123",
		Email:         "first@example.com",
		EmailVerified: false,
		Profile: map[string]any{
			"name": "First User",
		},
	})
	if err != nil {
		t.Fatalf("insert identity: %v", err)
	}
	if identity.ID != "identity_1" || identity.EmailVerified || identity.Profile["name"] != "First User" {
		t.Fatalf("unexpected inserted identity: %#v", identity)
	}

	updated, err := st.UpsertUserIdentity(ctx, store.UpsertUserIdentityInput{
		ID:            "identity_ignored",
		UserID:        "user_oidc",
		Provider:      "kirari",
		Subject:       "sub-123",
		Email:         "verified@example.com",
		EmailVerified: true,
		Profile: map[string]any{
			"name": "Verified User",
		},
		LastLoginAt: &lastLogin,
	})
	if err != nil {
		t.Fatalf("update identity: %v", err)
	}
	if updated.ID != "identity_1" {
		t.Fatalf("upsert should keep original id on provider/subject conflict: %#v", updated)
	}
	if updated.Email != "verified@example.com" || !updated.EmailVerified || updated.Profile["name"] != "Verified User" {
		t.Fatalf("unexpected updated identity: %#v", updated)
	}
	if updated.LastLoginAt == nil || !updated.LastLoginAt.Equal(lastLogin) {
		t.Fatalf("unexpected identity last login: %#v", updated.LastLoginAt)
	}

	got, err := st.GetUserIdentity(ctx, "kirari", "sub-123")
	if err != nil {
		t.Fatalf("get identity: %v", err)
	}
	if got.Email != updated.Email {
		t.Fatalf("unexpected fetched identity: %#v", got)
	}

	items, err := st.ListUserIdentities(ctx, "user_oidc")
	if err != nil {
		t.Fatalf("list identities: %v", err)
	}
	if len(items) != 1 || items[0].Provider != "kirari" {
		t.Fatalf("unexpected identities: %#v", items)
	}

	if _, err := st.GetUserIdentity(ctx, "kirari", "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing identity, got %v", err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.DB().Close()
	})
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return st
}
