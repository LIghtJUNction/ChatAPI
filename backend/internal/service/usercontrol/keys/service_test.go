package keys_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	"github.com/zyf2007/ChatAPI/internal/repository/repositorycontract"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	appkeysvc "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkeysvc "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	userkeys "github.com/zyf2007/ChatAPI/internal/service/usercontrol/keys"
)

func TestKeysServiceCreateListRevoke(t *testing.T) {
	st := openKeysStore(t)
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, common.CreateUserInput{ID: "user_a", Username: "alice", Email: "alice@example.com", IsActive: true}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	accountService := account.NewService(st)
	_ = accountService
	appKeys := appkeysvc.NewService(st, "test-master-key")
	modelKeys := modelkeysvc.NewService(st, "test-master-key")
	svc := userkeys.New(userkeys.Deps{Keys: st, AppKeys: appKeys, ModelKeys: modelKeys})

	expiresAt := time.Now().UTC().Add(2 * time.Hour)
	appItem, rawAppKey, err := svc.CreateAppKey(ctx, " user_a ", " my-app ", []string{"requests:read"}, map[string]any{"max_requests_per_minute": 5}, &expiresAt)
	if err != nil {
		t.Fatalf("create app key: %v", err)
	}
	if rawAppKey == "" || appItem.UserID != "user_a" {
		t.Fatalf("unexpected app key: %#v raw=%q", appItem, rawAppKey)
	}
	if revealed, err := svc.RevealAppKey(ctx, "user_a", appItem.ID); err != nil || revealed != rawAppKey {
		t.Fatalf("reveal app key: value=%q err=%v", revealed, err)
	}

	modelItem, rawModelKey, err := svc.CreateModelKey(ctx, " user_a ", " my-model ", " demo-model ")
	if err != nil {
		t.Fatalf("create model key: %v", err)
	}
	if rawModelKey == "" || modelItem.Model != "demo-model" {
		t.Fatalf("unexpected model key: %#v raw=%q", modelItem, rawModelKey)
	}
	if revealed, err := svc.RevealModelKey(ctx, "user_a", modelItem.ID); err != nil || revealed != rawModelKey {
		t.Fatalf("reveal model key: value=%q err=%v", revealed, err)
	}

	appList, err := svc.ListAppKeys(ctx, "user_a")
	if err != nil || len(appList) != 1 {
		t.Fatalf("list app keys: len=%d err=%v", len(appList), err)
	}
	if appList[0].RevokedAt != nil {
		t.Fatalf("app key should be active before revoke: %#v", appList[0])
	}
	modelList, err := svc.ListModelKeys(ctx, "user_a")
	if err != nil || len(modelList) != 1 {
		t.Fatalf("list model keys: len=%d err=%v", len(modelList), err)
	}
	if modelList[0].RevokedAt != nil {
		t.Fatalf("model key should be active before revoke: %#v", modelList[0])
	}

	if err := svc.RevokeAppKey(ctx, " user_a ", " "+appItem.ID+" "); err != nil {
		t.Fatalf("revoke app key: %v", err)
	}
	if err := svc.RevokeModelKey(ctx, " user_a ", " "+modelItem.ID+" "); err != nil {
		t.Fatalf("revoke model key: %v", err)
	}

	appList, err = svc.ListAppKeys(ctx, "user_a")
	if err != nil || len(appList) != 0 {
		t.Fatalf("revoked app key should be hidden from list: len=%d err=%v item=%#v", len(appList), err, appList)
	}
	modelList, err = svc.ListModelKeys(ctx, "user_a")
	if err != nil || len(modelList) != 0 {
		t.Fatalf("revoked model key should be hidden from list: len=%d err=%v item=%#v", len(modelList), err, modelList)
	}
}

func openKeysStore(t *testing.T) repositorycontract.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}
	return st
}
