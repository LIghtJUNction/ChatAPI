package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zyf2007/ChatAPI/internal/actor"
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
	appkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	pendingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
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

func TestRouterWorkspaceWebSocketReceivesTimelineEventOnAbort(t *testing.T) {
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
	pending := pendingsvc.NewPendingRegistry()
	pending.Logger = logFactory.Layer(logging.LayerPending)
	turnService := &turnsvc.Service{
		Submitter: &turnsvc.Submitter{
			Store:    st,
			Pending:  pending,
			Realtime: workspacesvc.NewRealtimePublisher(workspaceHub),
		},
		Pending:            pending,
		Store:              st,
		OwnerIDFromContext: actor.OwnerIDFromContext,
		ActorFromContext:   actor.FromContext,
		Logger:             logFactory.Layer(logging.LayerTurn),
	}
	modelKeyService := modelkey.NewService(st, "test-master-key")
	appKeyService := appkey.NewService(st)

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
		Turn:          turnService,
		ModelAPIKeys:  modelKeyService,
		AppAPIKeys:    appKeyService,
		Workspace:     workspaceService,
		WorkspaceHub:  workspaceHub,
		LoggerFactory: logFactory,
	}))
	defer server.Close()

	cookie := loginAndGetCookie(t, server.URL, "alice@example.com", "user-pass")
	_, modelKey, err := modelKeyService.CreateKey(context.Background(), "user_a", "user-a-model", "demo-model")
	if err != nil {
		t.Fatalf("create model key: %v", err)
	}

	wsURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	wsURL.Scheme = "ws"
	wsURL.Path = "/api/ws"

	header := http.Header{}
	header.Add("Cookie", cookie.Name+"="+cookie.Value)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var snapshot map[string]any
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	resultCh := make(chan map[string]any, 1)
	go func() {
		resultCh <- postJSONWithHeaders(t, server.URL+"/v1/responses", map[string]any{
			"model": "demo-model",
			"input": "please abort me",
		}, map[string]string{
			"Authorization": "Bearer " + modelKey,
		}, http.StatusOK)
	}()

	request := waitForRequestForOwner(t, queryService, "user_a")
	postJSONWithCookie(t, server.URL+"/api/conversations/"+request.ConversationID+"/abort", map[string]any{
		"error": "manual stop",
	}, cookie, http.StatusOK)
	<-resultCh

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read ws event: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("decode ws event: %v", err)
		}
		if payload["type"] != "timeline_item_append" {
			continue
		}
		item, _ := payload["item"].(map[string]any)
		if item["kind"] != "system_event" {
			t.Fatalf("unexpected timeline item: %#v", payload)
		}
		event, _ := item["event"].(map[string]any)
		if event["type"] != "request_aborted" {
			t.Fatalf("unexpected system event payload: %#v", payload)
		}
		return
	}
	t.Fatal("expected timeline_item_append event")
}

func TestRouterConversationTimelineIncludesSystemEvent(t *testing.T) {
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
	pending := pendingsvc.NewPendingRegistry()
	pending.Logger = logFactory.Layer(logging.LayerPending)
	turnService := &turnsvc.Service{
		Submitter: &turnsvc.Submitter{
			Store:    st,
			Pending:  pending,
			Realtime: workspacesvc.NewRealtimePublisher(workspaceHub),
		},
		Pending:            pending,
		Store:              st,
		OwnerIDFromContext: actor.OwnerIDFromContext,
		ActorFromContext:   actor.FromContext,
		Logger:             logFactory.Layer(logging.LayerTurn),
	}
	modelKeyService := modelkey.NewService(st, "test-master-key")
	appKeyService := appkey.NewService(st)

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
		Turn:          turnService,
		ModelAPIKeys:  modelKeyService,
		AppAPIKeys:    appKeyService,
		Workspace:     workspaceService,
		WorkspaceHub:  workspaceHub,
		LoggerFactory: logFactory,
	}))
	defer server.Close()

	cookie := loginAndGetCookie(t, server.URL, "alice@example.com", "user-pass")
	_, modelKey, err := modelKeyService.CreateKey(context.Background(), "user_a", "user-a-model", "demo-model")
	if err != nil {
		t.Fatalf("create model key: %v", err)
	}

	resultCh := make(chan map[string]any, 1)
	go func() {
		resultCh <- postJSONWithHeaders(t, server.URL+"/v1/responses", map[string]any{
			"model": "demo-model",
			"input": "please abort me",
		}, map[string]string{
			"Authorization": "Bearer " + modelKey,
		}, http.StatusOK)
	}()

	request := waitForRequestForOwner(t, queryService, "user_a")
	postJSONWithCookie(t, server.URL+"/api/conversations/"+request.ConversationID+"/abort", map[string]any{
		"error": "manual stop",
	}, cookie, http.StatusOK)
	<-resultCh

	timelineResp := getJSONWithCookie(t, server.URL+"/api/conversations/"+request.ConversationID+"/timeline", cookie, http.StatusOK)
	items, ok := timelineResp["items"].([]any)
	if !ok || len(items) < 2 {
		t.Fatalf("unexpected timeline response: %#v", timelineResp)
	}

	last, ok := items[len(items)-1].(map[string]any)
	if !ok {
		t.Fatalf("unexpected last timeline item: %#v", timelineResp)
	}
	if last["kind"] != "system_event" {
		t.Fatalf("expected system event last item, got %#v", last)
	}
	event, ok := last["event"].(map[string]any)
	if !ok {
		t.Fatalf("expected event payload, got %#v", last)
	}
	if event["type"] != "request_aborted" || event["detail"] != "manual stop" {
		t.Fatalf("unexpected event payload: %#v", event)
	}
}
