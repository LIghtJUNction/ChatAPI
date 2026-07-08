package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zyf2007/ChatAPI/internal/config"
	httpapi "github.com/zyf2007/ChatAPI/internal/http"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/identity"
	localauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/local"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/verification"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	workspacesvc "github.com/zyf2007/ChatAPI/internal/service/chat/workspace"
)

func TestRouterWorkspaceWebSocketUpgradeWithSession(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}

	userPasswordHash, err := passwordHash("user-pass")
	if err != nil {
		t.Fatalf("user password hash: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:           "user_a",
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: userPasswordHash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := st.UpsertUserIdentity(context.Background(), common.UpsertUserIdentityInput{
		ID:            "identity_local_alice",
		UserID:        "user_a",
		Provider:      "local",
		Subject:       "alice@example.com",
		Email:         "alice@example.com",
		EmailVerified: true,
	}); err != nil {
		t.Fatalf("create user identity: %v", err)
	}

	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("new logger factory: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)

	policies := policy.NewService()
	sessionService, err := session.NewService(session.Config{Secret: "01234567890123456789012345678901"})
	if err != nil {
		t.Fatalf("new session service: %v", err)
	}
	verificationService := verification.NewService(st, &memorySender{})
	accountService := account.NewService(st)
	localService := localauth.NewService(accountService, st, policies, sessionService, verificationService)
	localService.Logger = logFactory.Layer(logging.LayerAuth)
	identityService := identity.NewService(accountService)
	queryService := &turnquerysvc.Service{Store: st, Logger: logFactory.Layer(logging.LayerTurnQuery)}
	workspaceService := workspacesvc.New(queryService)
	workspaceHub := workspacesvc.NewHub(workspaceService)

	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	server := httptest.NewServer(httpapi.NewRouter(httpapi.RouterDeps{
		Config:        cfg,
		ChatRepo:      st,
		AuthRepo:      st,
		ConfigRepo:    st,
		StorageRepo:   st,
		AuditRepo:     st,
		PlatformRepo:  st,
		LocalAuth:     localService,
		Verification:  verificationService,
		Policy:        policies,
		Accounts:      accountService,
		Identity:      identityService,
		UserSessions:  sessionService,
		Query:         queryService,
		Workspace:     workspaceService,
		WorkspaceHub:  workspaceHub,
		LoggerFactory: logFactory,
	}))
	defer server.Close()

	cookie := loginAndGetCookie(t, server.URL, "alice@example.com", "user-pass")

	wsURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	wsURL.Scheme = "ws"
	wsURL.Path = "/api/ws"

	header := http.Header{}
	header.Add("Cookie", cookie.Name+"="+cookie.Value)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial websocket: %v (status=%d)", err, resp.StatusCode)
		}
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var snapshot map[string]any
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if snapshot["type"] != "snapshot" {
		t.Fatalf("unexpected first websocket event: %#v", snapshot)
	}
}
