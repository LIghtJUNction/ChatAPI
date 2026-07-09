package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
	httpapi "github.com/zyf2007/ChatAPI/internal/http"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	"github.com/zyf2007/ChatAPI/internal/service/admincontrol"
	auditsvc "github.com/zyf2007/ChatAPI/internal/service/audit"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
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
)

func TestRouterUserFlow(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}

	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("new logger factory: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)

	userPasswordHash, err := passwordHash("user-pass")
	if err != nil {
		t.Fatalf("user password hash: %v", err)
	}
	otherPasswordHash, err := passwordHash("other-pass")
	if err != nil {
		t.Fatalf("other password hash: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:           "user_a",
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: userPasswordHash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user a: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:           "user_b",
		Username:     "bob",
		Email:        "bob@example.com",
		PasswordHash: otherPasswordHash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user b: %v", err)
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
	modelKeyService := modelkey.NewService(st, "test-master-key")
	appKeyService := appkey.NewService(st)
	appKeyService.Logger = logFactory.Layer(logging.LayerAudit)
	pending := pendingsvc.NewPendingRegistry()
	pending.Logger = logFactory.Layer(logging.LayerPending)
	turnService := &turnsvc.Service{
		Submitter: &turnsvc.Submitter{
			Store:   st,
			Pending: pending,
		},
		Pending:            pending,
		Store:              st,
		OwnerIDFromContext: actor.OwnerIDFromContext,
		ActorFromContext:   actor.FromContext,
		Events:             noopEvents,
		Logger:             logFactory.Layer(logging.LayerTurn),
	}
	queryService := &turnquerysvc.Service{Store: st, Logger: logFactory.Layer(logging.LayerTurnQuery)}
	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	cfg.GeetestCaptchaID = "captcha-id"
	accessSettings := authaccess.NewSettingsService(st, authaccess.Settings{
		GlobalRateLimitRequests: cfg.AccessRateLimitRequests,
		GlobalRateLimitWindow:   cfg.AccessRateLimitWindow,
	})
	adminControl := admincontrol.New(admincontrol.Deps{
		Accounts:       accountService,
		Query:          queryService,
		Turn:           turnService,
		ChatStore:      st,
		StorageStore:   st,
		KeyStore:       st,
		AccessSettings: accessSettings,
	})

	server := httptest.NewServer(httpapi.NewRouter(httpapi.RouterDeps{
		Config:         cfg,
		ChatRepo:       st,
		AuthRepo:       st,
		ConfigRepo:     st,
		StorageRepo:    st,
		AuditRepo:      st,
		PlatformRepo:   st,
		Turn:           turnService,
		Query:          queryService,
		ModelAPIKeys:   modelKeyService,
		AppAPIKeys:     appKeyService,
		LocalAuth:      localService,
		Verification:   verificationService,
		Policy:         policies,
		AccessSettings: accessSettings,
		Accounts:       accountService,
		AdminControl:   adminControl,
		Audit:          auditsvc.NewService(st),
		Identity:       identityService,
		UserSessions:   sessionService,
		LoggerFactory:  logFactory,
	}))
	defer server.Close()

	userCookie := loginAndGetCookie(t, server.URL, "alice@example.com", "user-pass")
	otherCookie := loginAndGetCookie(t, server.URL, "bob@example.com", "other-pass")

	sessionResp := getJSONWithCookie(t, server.URL+"/api/auth/session", userCookie, http.StatusOK)
	if authenticated, _ := sessionResp["authenticated"].(bool); !authenticated {
		t.Fatalf("unexpected session response: %#v", sessionResp)
	}
	userInfo := sessionResp["user"].(map[string]any)
	if userInfo["username"] != "alice" || userInfo["role"] != "user" {
		t.Fatalf("unexpected session user: %#v", sessionResp)
	}
	if geetestEnabled, _ := sessionResp["geetest_enabled"].(bool); !geetestEnabled {
		t.Fatalf("expected geetest enabled in session response: %#v", sessionResp)
	}

	_, modelKeyA, err := modelKeyService.CreateKey(context.Background(), "user_a", "user-a-model", "demo-model")
	if err != nil {
		t.Fatalf("create model key a: %v", err)
	}

	firstResultCh := make(chan map[string]any, 1)
	go func() {
		firstResultCh <- postJSONWithHeaders(t, server.URL+"/v1/responses", map[string]any{
			"model": "demo-model",
			"input": "first pending request",
		}, map[string]string{
			"Authorization": "Bearer " + modelKeyA,
		}, http.StatusOK)
	}()
	firstRequest := waitForRequestForOwner(t, queryService, "user_a")

	listResp := getJSONWithCookie(t, server.URL+"/api/conversations", userCookie, http.StatusOK)
	if len(listResp["items"].([]any)) != 1 {
		t.Fatalf("unexpected conversations list: %#v", listResp)
	}

	postJSONWithCookie(t, server.URL+"/api/chat/output/complete", map[string]any{
		"conversation_id": firstRequest.ConversationID,
		"text":            "done from session",
		"mode":            "assistant_message",
	}, userCookie, http.StatusOK)
	firstFinal := <-firstResultCh
	if firstFinal["output_text"] != "done from session" {
		t.Fatalf("unexpected completed response: %#v", firstFinal)
	}

	msgResp := getJSONWithCookie(t, server.URL+"/api/conversations/"+firstRequest.ConversationID+"/messages", userCookie, http.StatusOK)
	if len(msgResp["items"].([]any)) != 2 {
		t.Fatalf("unexpected messages: %#v", msgResp)
	}
	status, body := getTextWithCookie(t, server.URL+"/api/conversations/"+firstRequest.ConversationID+"/messages", otherCookie)
	if status != http.StatusForbidden || !strings.Contains(body, "forbidden") {
		t.Fatalf("unexpected other user conversation access: status=%d body=%q", status, body)
	}

	secondResultCh := make(chan map[string]any, 1)
	go func() {
		secondResultCh <- postJSONWithHeaders(t, server.URL+"/v1/responses", map[string]any{
			"model": "demo-model",
			"input": "second pending request",
		}, map[string]string{
			"Authorization": "Bearer " + modelKeyA,
		}, http.StatusOK)
	}()
	secondRequest := waitForPendingRequestForOwnerExcluding(t, queryService, "user_a", firstRequest.ConversationID)
	deleteStatus, deleteBody := deleteTextWithCookie(t, server.URL+"/api/conversations/"+secondRequest.ConversationID, userCookie)
	if deleteStatus != http.StatusConflict || !strings.Contains(deleteBody, "waiting conversation cannot be deleted") {
		t.Fatalf("unexpected delete waiting conversation result: status=%d body=%q", deleteStatus, deleteBody)
	}
	postJSONWithCookie(t, server.URL+"/api/conversations/"+secondRequest.ConversationID+"/abort", map[string]any{
		"error": "stopped",
	}, userCookie, http.StatusOK)
	secondFinal := <-secondResultCh
	if _, ok := secondFinal["error"]; !ok {
		t.Fatalf("unexpected aborted response: %#v", secondFinal)
	}

	appKeysResp := getJSONWithCookie(t, server.URL+"/api/user/app-keys", userCookie, http.StatusOK)
	if len(appKeysResp["items"].([]any)) != 0 {
		t.Fatalf("unexpected initial app keys: %#v", appKeysResp)
	}
	createAppResp := postJSONWithCookie(t, server.URL+"/api/user/app-keys", map[string]any{
		"name":            "debug-app",
		"scopes":          []string{"requests:read"},
		"resource_limits": map[string]any{"max_requests_per_minute": 10},
	}, userCookie, http.StatusCreated)
	appKey := createAppResp["api_key"].(map[string]any)
	deleteJSONWithCookie(t, server.URL+"/api/user/app-keys/"+appKey["id"].(string), userCookie, http.StatusOK)

	modelKeysResp := getJSONWithCookie(t, server.URL+"/api/user/model-keys", userCookie, http.StatusOK)
	if len(modelKeysResp["items"].([]any)) != 1 {
		t.Fatalf("unexpected initial model keys: %#v", modelKeysResp)
	}
	createModelResp := postJSONWithCookie(t, server.URL+"/api/user/model-keys", map[string]any{
		"name":  "extra-model",
		"model": "demo-model-2",
	}, userCookie, http.StatusCreated)
	modelKey := createModelResp["model_key"].(map[string]any)
	deleteJSONWithCookie(t, server.URL+"/api/user/model-keys/"+modelKey["id"].(string), userCookie, http.StatusOK)

	identitiesResp := getJSONWithCookie(t, server.URL+"/api/user/identities", userCookie, http.StatusOK)
	if len(identitiesResp["items"].([]any)) != 1 {
		t.Fatalf("unexpected identities: %#v", identitiesResp)
	}

	configResp := getJSONWithCookie(t, server.URL+"/api/user/config", userCookie, http.StatusOK)
	if configResp["ok"] != true {
		t.Fatalf("unexpected initial user config: %#v", configResp)
	}
	configResp = postJSONWithCookie(t, server.URL+"/api/user/config", map[string]any{
		"ntfy_url_enabled":                  true,
		"ntfy_url":                          "https://ntfy.sh/alice",
		"messages_per_minute_limit_enabled": true,
		"messages_per_minute_limit":         3,
	}, userCookie, http.StatusOK)
	if configResp["ntfy_url"] != "https://ntfy.sh/alice" {
		t.Fatalf("unexpected saved user config: %#v", configResp)
	}

	rulesResp := getJSONWithCookie(t, server.URL+"/api/config/automation-rules", userCookie, http.StatusOK)
	if len(rulesResp["rules"].([]any)) != 0 {
		t.Fatalf("unexpected initial automation rules: %#v", rulesResp)
	}
	rulesResp = postJSONWithCookie(t, server.URL+"/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{{
			"enabled": true,
			"name":    "rule-1",
			"match":   "hello",
		}},
	}, userCookie, http.StatusOK)
	if len(rulesResp["rules"].([]any)) != 1 {
		t.Fatalf("unexpected saved automation rules: %#v", rulesResp)
	}

	postJSONWithCookie(t, server.URL+"/api/user/password", map[string]any{
		"password": "user-pass-2",
	}, userCookie, http.StatusOK)
	status, body = postTextWithHeaders(t, server.URL+"/api/auth/login", map[string]any{
		"identifier": "alice@example.com",
		"password":   "user-pass",
	}, nil)
	if status != http.StatusUnauthorized || !strings.Contains(body, "invalid credentials") {
		t.Fatalf("unexpected old password login result: status=%d body=%q", status, body)
	}
	loginAndGetCookie(t, server.URL, "alice@example.com", "user-pass-2")

	pruneResp := postJSONWithCookie(t, server.URL+"/api/conversations/prune", map[string]any{
		"keep_count": 0,
	}, userCookie, http.StatusOK)
	if int(pruneResp["deleted_count"].(float64)) < 2 {
		t.Fatalf("unexpected prune response: %#v", pruneResp)
	}
}

func waitForPendingRequestForOwnerExcluding(t *testing.T, query *turnquerysvc.Service, ownerID string, excludeConversationID string) common.Request {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, err := query.ListRequestsForOwner(context.Background(), ownerID)
		if err == nil {
			for _, item := range items {
				if item.ConversationID == excludeConversationID {
					continue
				}
				if strings.EqualFold(item.Status, "pending") || strings.EqualFold(item.Status, "waiting") || item.Status == "" {
					return item
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for pending request")
	return common.Request{}
}

func deleteTextWithCookie(t *testing.T, url string, cookie *http.Cookie) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(cookie)
	setSameOriginHeader(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete %s: %v", url, err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(bodyBytes)
}
