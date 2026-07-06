package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

type NewStoreFunc func(t *testing.T) store.Store

const (
	httpStatusOK        = 200
	httpStatusForbidden = 403
)

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

func RunAPIKeyRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("app_api_keys", func(t *testing.T) {
		testAppAPIKeyRepositoryCreatesListsUsesAndRevokes(t, newStore)
	})
	t.Run("app_api_key_audit_logs", func(t *testing.T) {
		testAppAPIKeyRepositoryAuditsRequests(t, newStore)
	})
	t.Run("model_api_keys", func(t *testing.T) {
		testModelAPIKeyRepositoryCreatesListsUsesAndRevokes(t, newStore)
	})
}

func RunAuditRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("audit_logs", func(t *testing.T) {
		testAuditRepositoryCreatesFiltersAndLimitsLogs(t, newStore)
	})
}

func RunAutomationRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("automation_rules", func(t *testing.T) {
		testAutomationRuleRepositoryReplacesByScope(t, newStore)
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

func testAuditRepositoryCreatesFiltersAndLimitsLogs(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	first, err := st.CreateAuditLog(ctx, store.CreateAuditLogInput{
		ID:           "audit_1",
		ActorUserID:  "user_audit",
		ActorRole:    "user",
		ActorSource:  "session",
		EventType:    "user.identity",
		ResourceType: "user_identity",
		ResourceID:   "identity_1",
		Action:       "unlink",
		Outcome:      "success",
		IPAddress:    "127.0.0.1",
		UserAgent:    "contract-test",
		Metadata: map[string]any{
			"safe":        "value",
			"status_code": float64(200),
		},
	})
	if err != nil {
		t.Fatalf("create first audit log: %v", err)
	}
	if first.ID != "audit_1" || first.CreatedAt.IsZero() || first.Metadata["safe"] != "value" {
		t.Fatalf("unexpected created audit log: %#v", first)
	}
	if _, err := st.CreateAuditLog(ctx, store.CreateAuditLogInput{
		ID:           "audit_2",
		ActorUserID:  "other_user",
		ActorRole:    "admin",
		ActorSource:  "session",
		EventType:    "admin.runtime",
		ResourceType: "runtime",
		ResourceID:   "runtime",
		Action:       "gc",
		Outcome:      "success",
		Metadata: map[string]any{
			"freed": float64(123),
		},
	}); err != nil {
		t.Fatalf("create second audit log: %v", err)
	}
	if _, err := st.CreateAuditLog(ctx, store.CreateAuditLogInput{
		ID:           "audit_3",
		ActorUserID:  "user_audit",
		ActorRole:    "user",
		ActorSource:  "session",
		EventType:    "user.identity",
		ResourceType: "user_identity",
		ResourceID:   "identity_2",
		Action:       "unlink",
		Outcome:      "failure",
	}); err != nil {
		t.Fatalf("create third audit log: %v", err)
	}

	filtered, err := st.ListAuditLogs(ctx, store.ListAuditLogsInput{
		Limit:       10,
		EventType:   "user.identity",
		ActorUserID: "user_audit",
	})
	if err != nil {
		t.Fatalf("list filtered audit logs: %v", err)
	}
	if len(filtered) != 2 || filtered[0].ID != "audit_3" || filtered[1].ID != "audit_1" {
		t.Fatalf("unexpected filtered audit logs: %#v", filtered)
	}
	if filtered[1].Metadata["safe"] != "value" {
		t.Fatalf("unexpected filtered metadata: %#v", filtered[1])
	}

	limited, err := st.ListAuditLogs(ctx, store.ListAuditLogsInput{Limit: 1})
	if err != nil {
		t.Fatalf("list limited audit logs: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != "audit_3" {
		t.Fatalf("unexpected limited audit logs: %#v", limited)
	}
}

func testAutomationRuleRepositoryReplacesByScope(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	initial, err := st.ReplaceAutomationRulesForUser(ctx, "user_rules", nil, []store.UpsertAutomationRuleInput{
		{
			ID:      "rule_a",
			UserID:  "user_rules",
			Enabled: true,
			Payload: map[string]any{
				"id":      "rule_a",
				"enabled": true,
				"name":    "Rule A",
			},
		},
		{
			ID:      "rule_b",
			UserID:  "user_rules",
			Enabled: false,
			Payload: map[string]any{
				"id":      "rule_b",
				"enabled": false,
				"name":    "Rule B",
			},
		},
	})
	if err != nil {
		t.Fatalf("replace initial automation rules: %v", err)
	}
	if len(initial) != 2 {
		t.Fatalf("expected two initial rules, got %#v", initial)
	}

	replaceIDs := map[string]struct{}{"rule_a": {}}
	scoped, err := st.ReplaceAutomationRulesForUser(ctx, "user_rules", replaceIDs, []store.UpsertAutomationRuleInput{
		{
			ID:      "rule_a",
			UserID:  "user_rules",
			Enabled: false,
			Payload: map[string]any{
				"id":      "rule_a",
				"enabled": false,
				"name":    "Rule A2",
			},
		},
	})
	if err != nil {
		t.Fatalf("replace scoped automation rule: %v", err)
	}
	if len(scoped) != 2 {
		t.Fatalf("expected scoped replace to keep other rules, got %#v", scoped)
	}
	byID := map[string]store.AutomationRule{}
	for _, item := range scoped {
		byID[item.ID] = item
	}
	if byID["rule_a"].Payload["name"] != "Rule A2" || byID["rule_a"].Enabled {
		t.Fatalf("expected rule_a to be replaced, got %#v", byID["rule_a"])
	}
	if byID["rule_b"].Payload["name"] != "Rule B" || byID["rule_b"].Enabled {
		t.Fatalf("expected rule_b to remain disabled and unchanged, got %#v", byID["rule_b"])
	}

	otherUser, err := st.ListAutomationRulesByUser(ctx, "other_user")
	if err != nil {
		t.Fatalf("list other user automation rules: %v", err)
	}
	if len(otherUser) != 0 {
		t.Fatalf("expected other user rules to remain isolated: %#v", otherUser)
	}
}

func testAppAPIKeyRepositoryCreatesListsUsesAndRevokes(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	expiresAt := time.Date(2026, 7, 7, 1, 2, 3, 0, time.UTC)
	created, err := st.CreateAppAPIKey(ctx, store.CreateAppAPIKeyInput{
		ID:        "appkey_1",
		UserID:    "user_keys",
		Name:      "automation",
		KeyHash:   "hash",
		KeyPrefix: "ak-demo",
		Scopes:    []string{"requests:read", "requests:respond"},
		ResourceLimits: map[string]any{
			"max_requests_per_minute": float64(10),
			"allowed_source_ips":      []any{"127.0.0.1"},
		},
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("create app api key: %v", err)
	}
	if created.ID != "appkey_1" || created.UserID != "user_keys" || len(created.Scopes) != 2 || created.ExpiresAt == nil {
		t.Fatalf("unexpected created app key: %#v", created)
	}

	byPrefix, err := st.GetAppAPIKeyByPrefix(ctx, "ak-demo")
	if err != nil {
		t.Fatalf("get app key by prefix: %v", err)
	}
	if byPrefix.ID != created.ID || byPrefix.ResourceLimits["max_requests_per_minute"].(float64) != 10 {
		t.Fatalf("unexpected app key by prefix: %#v", byPrefix)
	}

	usedAt := time.Date(2026, 7, 8, 4, 5, 6, 0, time.UTC)
	if err := st.UpdateAppAPIKeyLastUsedAt(ctx, created.ID, usedAt); err != nil {
		t.Fatalf("update app key last used: %v", err)
	}
	items, err := st.ListAppAPIKeysByUser(ctx, "user_keys")
	if err != nil {
		t.Fatalf("list app keys: %v", err)
	}
	if len(items) != 1 || items[0].LastUsedAt == nil || !items[0].LastUsedAt.Equal(usedAt) {
		t.Fatalf("unexpected app key list after last used: %#v", items)
	}

	if err := st.RevokeAppAPIKey(ctx, created.ID, "other_user"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound revoking other user key, got %v", err)
	}
	if err := st.RevokeAppAPIKey(ctx, created.ID, "user_keys"); err != nil {
		t.Fatalf("revoke app key: %v", err)
	}
	revoked, err := st.GetAppAPIKeyByPrefix(ctx, "ak-demo")
	if err != nil {
		t.Fatalf("get revoked app key: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatalf("expected revoked_at after revoke: %#v", revoked)
	}
	if err := st.RevokeAppAPIKey(ctx, created.ID, "user_keys"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound revoking already revoked key, got %v", err)
	}
}

func testAppAPIKeyRepositoryAuditsRequests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	if err := st.CreateAppAPIKeyAuditLog(ctx, store.AppAPIKeyAuditLog{
		ID:          "applog_1",
		AppAPIKeyID: "appkey_1",
		UserID:      "user_audit",
		Route:       "/api/app/me",
		StatusCode:  httpStatusOK,
		ErrorCode:   "",
		CreatedAt:   time.Date(2026, 7, 9, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create app audit log: %v", err)
	}
	if err := st.CreateAppAPIKeyAuditLog(ctx, store.AppAPIKeyAuditLog{
		ID:          "applog_2",
		AppAPIKeyID: "appkey_2",
		UserID:      "other_user",
		Route:       "/api/app/requests",
		StatusCode:  httpStatusForbidden,
		ErrorCode:   "forbidden",
		CreatedAt:   time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create second app audit log: %v", err)
	}
	items, err := st.ListAppAPIKeyAuditLogs(ctx, store.ListAppAPIKeyAuditLogsInput{UserID: "user_audit", Limit: 10})
	if err != nil {
		t.Fatalf("list filtered app audit logs: %v", err)
	}
	if len(items) != 1 || items[0].ID != "applog_1" || items[0].StatusCode != httpStatusOK {
		t.Fatalf("unexpected filtered app audit logs: %#v", items)
	}
	all, err := st.ListAppAPIKeyAuditLogs(ctx, store.ListAppAPIKeyAuditLogsInput{Limit: 10})
	if err != nil {
		t.Fatalf("list app audit logs: %v", err)
	}
	if len(all) != 2 || all[0].ID != "applog_2" || all[1].ID != "applog_1" {
		t.Fatalf("unexpected app audit log ordering: %#v", all)
	}
}

func testModelAPIKeyRepositoryCreatesListsUsesAndRevokes(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	created, err := st.CreateModelAPIKey(ctx, store.CreateModelAPIKeyInput{
		ID:            "modelkey_1",
		UserID:        "user_model_keys",
		Name:          "default virtual model",
		KeyCiphertext: "ciphertext",
		KeyPrefix:     "sk-demo",
		Model:         "chatapi-demo",
	})
	if err != nil {
		t.Fatalf("create model api key: %v", err)
	}
	if created.ID != "modelkey_1" || created.Model != "chatapi-demo" {
		t.Fatalf("unexpected created model key: %#v", created)
	}
	byPrefix, err := st.GetModelAPIKeyByPrefix(ctx, "sk-demo")
	if err != nil {
		t.Fatalf("get model key by prefix: %v", err)
	}
	if byPrefix.ID != created.ID || byPrefix.KeyCiphertext != "ciphertext" {
		t.Fatalf("unexpected model key by prefix: %#v", byPrefix)
	}
	byID, err := st.GetModelAPIKeyByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get model key by id: %v", err)
	}
	if byID.KeyPrefix != "sk-demo" {
		t.Fatalf("unexpected model key by id: %#v", byID)
	}

	usedAt := time.Date(2026, 7, 10, 4, 5, 6, 0, time.UTC)
	if err := st.UpdateModelAPIKeyLastUsedAt(ctx, created.ID, usedAt); err != nil {
		t.Fatalf("update model key last used: %v", err)
	}
	items, err := st.ListModelAPIKeysByUser(ctx, "user_model_keys")
	if err != nil {
		t.Fatalf("list model keys: %v", err)
	}
	if len(items) != 1 || items[0].LastUsedAt == nil || !items[0].LastUsedAt.Equal(usedAt) {
		t.Fatalf("unexpected model key list after last used: %#v", items)
	}

	if err := st.RevokeModelAPIKey(ctx, created.ID, "other_user"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound revoking other user model key, got %v", err)
	}
	if err := st.RevokeModelAPIKey(ctx, created.ID, "user_model_keys"); err != nil {
		t.Fatalf("revoke model key: %v", err)
	}
	revoked, err := st.GetModelAPIKeyByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get revoked model key: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatalf("expected revoked_at after model revoke: %#v", revoked)
	}
	if _, err := st.GetModelAPIKeyByPrefix(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing model prefix, got %v", err)
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
	if err := st.DeleteUserIdentity(ctx, updated.ID, "other_user"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting identity for wrong user, got %v", err)
	}
	if err := st.DeleteUserIdentity(ctx, updated.ID, "user_oidc"); err != nil {
		t.Fatalf("delete identity: %v", err)
	}
	if _, err := st.GetUserIdentity(ctx, "kirari", "sub-123"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected deleted identity to be missing, got %v", err)
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
