package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

type NewStoreFunc func(t *testing.T) store.Store

func RunUserRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("users", func(t *testing.T) {
		testUserRepositoryCreatesUpdatesAndListsUsers(t, newStore)
	})
	t.Run("user_identities", func(t *testing.T) {
		testUserIdentityRepositoryUpsertsByProviderSubject(t, newStore)
	})
}

func RunConfigRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("system_config", func(t *testing.T) {
		testConfigRepositoryUpsertsListsAndDeletesSystemConfig(t, newStore)
	})
	t.Run("user_config", func(t *testing.T) {
		testConfigRepositoryUpsertsListsAndDeletesUserConfig(t, newStore)
	})
}

func testUserRepositoryCreatesUpdatesAndListsUsers(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

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
	byUsername, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("get by username: %v", err)
	}
	if byUsername.ID != alice.ID || byUsername.Email != "alice@example.com" {
		t.Fatalf("unexpected user by username: %#v", byUsername)
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
	if _, err := st.GetUserByUsername(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing username, got %v", err)
	}
}

func testUserIdentityRepositoryUpsertsByProviderSubject(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
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

func testConfigRepositoryUpsertsListsAndDeletesSystemConfig(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	item, err := st.SetSystemConfig(ctx, store.SetSystemConfigInput{
		Key: "runtime.gc",
		Value: map[string]any{
			"gogc": 125,
		},
	})
	if err != nil {
		t.Fatalf("set system config: %v", err)
	}
	if item.Key != "runtime.gc" || item.Value["gogc"].(float64) != 125 {
		t.Fatalf("unexpected system config: %#v", item)
	}

	updated, err := st.SetSystemConfig(ctx, store.SetSystemConfigInput{
		Key: "runtime.gc",
		Value: map[string]any{
			"gogc":         80,
			"memory_limit": "512MiB",
		},
	})
	if err != nil {
		t.Fatalf("update system config: %v", err)
	}
	if updated.Value["gogc"].(float64) != 80 || updated.Value["memory_limit"] != "512MiB" {
		t.Fatalf("unexpected updated system config: %#v", updated)
	}
	if !updated.CreatedAt.Equal(item.CreatedAt) {
		t.Fatalf("upsert should keep created_at, before=%s after=%s", item.CreatedAt, updated.CreatedAt)
	}

	if _, err := st.SetSystemConfig(ctx, store.SetSystemConfigInput{
		Key: "storage.cleanup",
		Value: map[string]any{
			"enabled": true,
		},
	}); err != nil {
		t.Fatalf("set second system config: %v", err)
	}

	items, err := st.ListSystemConfigs(ctx)
	if err != nil {
		t.Fatalf("list system configs: %v", err)
	}
	if len(items) != 2 || items[0].Key != "runtime.gc" || items[1].Key != "storage.cleanup" {
		t.Fatalf("unexpected system config list: %#v", items)
	}

	if err := st.DeleteSystemConfig(ctx, "runtime.gc"); err != nil {
		t.Fatalf("delete system config: %v", err)
	}
	if _, err := st.GetSystemConfig(ctx, "runtime.gc"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func testConfigRepositoryUpsertsListsAndDeletesUserConfig(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	if _, err := st.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: "user_1",
		Key:    "workspace",
		Value: map[string]any{
			"compact": true,
		},
	}); err != nil {
		t.Fatalf("set user config: %v", err)
	}
	updated, err := st.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: "user_1",
		Key:    "workspace",
		Value: map[string]any{
			"compact": false,
			"theme":   "dark",
		},
	})
	if err != nil {
		t.Fatalf("update user config: %v", err)
	}
	if updated.UserID != "user_1" || updated.Key != "workspace" {
		t.Fatalf("unexpected user config identity: %#v", updated)
	}
	if updated.Value["compact"] != false || updated.Value["theme"] != "dark" {
		t.Fatalf("unexpected user config value: %#v", updated)
	}

	if _, err := st.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: "user_1",
		Key:    "notifications",
		Value: map[string]any{
			"email": false,
		},
	}); err != nil {
		t.Fatalf("set second user config: %v", err)
	}
	if _, err := st.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: "user_2",
		Key:    "workspace",
		Value: map[string]any{
			"compact": true,
		},
	}); err != nil {
		t.Fatalf("set other user config: %v", err)
	}

	items, err := st.ListUserConfigs(ctx, "user_1")
	if err != nil {
		t.Fatalf("list user configs: %v", err)
	}
	if len(items) != 2 || items[0].Key != "notifications" || items[1].Key != "workspace" {
		t.Fatalf("unexpected user config list: %#v", items)
	}

	if err := st.DeleteUserConfig(ctx, "user_1", "workspace"); err != nil {
		t.Fatalf("delete user config: %v", err)
	}
	if _, err := st.GetUserConfig(ctx, "user_1", "workspace"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	otherUserItem, err := st.GetUserConfig(ctx, "user_2", "workspace")
	if err != nil {
		t.Fatalf("other user config should remain: %v", err)
	}
	if otherUserItem.UserID != "user_2" {
		t.Fatalf("unexpected other user item: %#v", otherUserItem)
	}
}
