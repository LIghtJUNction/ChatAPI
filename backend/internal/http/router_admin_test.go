package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/config"
	httpapi "github.com/zyf/chatapi/internal/http"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/platform/password"
	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/service/account"
	auditsvc "github.com/zyf/chatapi/internal/service/audit"
	authadmin "github.com/zyf/chatapi/internal/service/auth/admin"
	"github.com/zyf/chatapi/internal/service/auth/authn/identity"
	localauth "github.com/zyf/chatapi/internal/service/auth/authn/local"
	"github.com/zyf/chatapi/internal/service/auth/authn/verification"
	appkey "github.com/zyf/chatapi/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf/chatapi/internal/service/auth/authz/modelkey"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	"github.com/zyf/chatapi/internal/service/auth/authz/session"
	chatadmin "github.com/zyf/chatapi/internal/service/chat/admin"
	pendingsvc "github.com/zyf/chatapi/internal/service/chat/pending"
	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
	turnquerysvc "github.com/zyf/chatapi/internal/service/chat/turnquery"
	usersvc "github.com/zyf/chatapi/internal/service/user"
	"github.com/zyf/chatapi/internal/store"
)

func TestRouterAdminFlow(t *testing.T) {
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

	adminPasswordHash, err := passwordHash("admin-pass")
	if err != nil {
		t.Fatalf("admin password hash: %v", err)
	}
	userPasswordHash, err := passwordHash("user-pass")
	if err != nil {
		t.Fatalf("user password hash: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "admin_user",
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: adminPasswordHash,
		Role:         "admin",
		IsActive:     true,
		LocalAdmin:   true,
	}); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "normal_user",
		Username:     "bob",
		Email:        "bob@example.com",
		PasswordHash: userPasswordHash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create normal user: %v", err)
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
	auditService := auditsvc.NewService(st)
	adminUserService := authadmin.NewService(accountService, st, policies)
	modelKeyService := modelkey.NewService(st, "test-master-key")
	appKeyService := appkey.NewService(st)
	appKeyService.Logger = logFactory.Layer(logging.LayerAudit)
	userService := usersvc.NewService(accountService, st, appKeyService, modelKeyService)

	pending := pendingsvc.NewPendingRegistry()
	turnService := &turnsvc.Service{
		Submitter: &turnsvc.Submitter{
			Store:    st,
			Pending:  pending,
			Realtime: noopRealtime{},
		},
		Pending:            pending,
		Store:              st,
		OwnerIDFromContext: actor.OwnerIDFromContext,
		ActorFromContext:   actor.FromContext,
		Logger:             logFactory.Layer(logging.LayerTurn),
	}
	queryService := &turnquerysvc.Service{Store: st, Logger: logFactory.Layer(logging.LayerTurnQuery)}
	adminChatService := chatadmin.NewService(queryService, turnService, st)

	_, modelKey, err := modelKeyService.CreateKey(context.Background(), "normal_user", "user-model", "demo-model")
	if err != nil {
		t.Fatalf("create model key: %v", err)
	}
	createdAppKey, _, err := appKeyService.CreateKey(context.Background(), "normal_user", "user-app", []string{"requests:read"}, nil, nil)
	if err != nil {
		t.Fatalf("create app key: %v", err)
	}
	createdModelKey, _, err := modelKeyService.CreateKey(context.Background(), "normal_user", "user-model-admin", "demo-model")
	if err != nil {
		t.Fatalf("create second model key: %v", err)
	}

	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	server := httptest.NewServer(httpapi.NewRouter(httpapi.RouterDeps{
		Config:        cfg,
		Turn:          turnService,
		Query:         queryService,
		ModelAPIKeys:  modelKeyService,
		AppAPIKeys:    appKeyService,
		LocalAuth:     localService,
		Verification:  verificationService,
		Policy:        policies,
		AdminUsers:    adminUserService,
		AdminChat:     adminChatService,
		Audit:         auditService,
		Identity:      identityService,
		Users:         userService,
		UserSessions:  sessionService,
		LoggerFactory: logFactory,
	}))
	defer server.Close()

	status, _ := getTextWithHeaders(t, server.URL+"/api/admin/users", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized admin access, got %d", status)
	}

	userCookie := loginAndGetCookie(t, server.URL, "bob@example.com", "user-pass")
	status, body := getTextWithCookie(t, server.URL+"/api/admin/users", userCookie)
	if status != http.StatusForbidden || !strings.Contains(body, "admin forbidden") {
		t.Fatalf("expected forbidden non-admin access, got status=%d body=%q", status, body)
	}

	adminCookie := loginAndGetCookie(t, server.URL, "admin@example.com", "admin-pass")

	requestDone := make(chan map[string]any, 1)
	go func() {
		requestDone <- postJSONWithHeaders(t, server.URL+"/v1/responses", map[string]any{
			"model": "demo-model",
			"input": "admin pending request",
		}, map[string]string{"Authorization": "Bearer " + modelKey}, http.StatusOK)
	}()
	request := waitForRequestForOwner(t, queryService, "normal_user")

	adminUsers := getJSONWithCookie(t, server.URL+"/api/admin/users", adminCookie, http.StatusOK)
	if int(adminUsers["count"].(float64)) < 2 {
		t.Fatalf("unexpected admin users response: %#v", adminUsers)
	}

	getJSONWithCookie(t, server.URL+"/api/admin/users/normal_user", adminCookie, http.StatusOK)
	appKeysResp := getJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/app-keys", adminCookie, http.StatusOK)
	if int(appKeysResp["count"].(float64)) != 1 {
		t.Fatalf("unexpected app keys response: %#v", appKeysResp)
	}
	modelKeysResp := getJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/model-keys", adminCookie, http.StatusOK)
	if int(modelKeysResp["count"].(float64)) < 2 {
		t.Fatalf("unexpected model keys response: %#v", modelKeysResp)
	}

	postJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/disable", map[string]any{}, adminCookie, http.StatusOK)
	status, body = postTextWithHeaders(t, server.URL+"/api/auth/login", map[string]any{
		"identifier": "bob@example.com",
		"password":   "user-pass",
	}, nil)
	if status != http.StatusForbidden || !strings.Contains(body, "user is disabled") {
		t.Fatalf("expected disabled login rejection: status=%d body=%q", status, body)
	}
	postJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/enable", map[string]any{}, adminCookie, http.StatusOK)
	putJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/password", map[string]any{
		"password": "user-pass-2",
	}, adminCookie, http.StatusOK)
	loginAndGetCookie(t, server.URL, "bob@example.com", "user-pass-2")

	deletePreview := getJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/delete-preview", adminCookie, http.StatusOK)
	if _, ok := deletePreview["preview"]; !ok {
		t.Fatalf("unexpected delete preview: %#v", deletePreview)
	}
	ownershipItems := getJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/ownership-items", adminCookie, http.StatusOK)
	if _, ok := ownershipItems["items"]; !ok {
		t.Fatalf("unexpected ownership items: %#v", ownershipItems)
	}

	adminRequests := getJSONWithCookie(t, server.URL+"/api/admin/requests", adminCookie, http.StatusOK)
	if int(adminRequests["count"].(float64)) == 0 {
		t.Fatalf("unexpected admin requests response: %#v", adminRequests)
	}
	getJSONWithCookie(t, server.URL+"/api/admin/requests/"+request.RequestID, adminCookie, http.StatusOK)
	getJSONWithCookie(t, server.URL+"/api/admin/conversations", adminCookie, http.StatusOK)
	getJSONWithCookie(t, server.URL+"/api/admin/conversations/"+request.ConversationID+"/messages", adminCookie, http.StatusOK)

	postJSONWithCookie(t, server.URL+"/api/admin/conversations/"+request.ConversationID+"/complete", map[string]any{
		"text": "admin completed",
		"mode": "assistant_message",
	}, adminCookie, http.StatusOK)
	final := <-requestDone
	if final["output_text"] != "admin completed" {
		t.Fatalf("unexpected final response: %#v", final)
	}

	deleteJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/app-keys/"+createdAppKey.ID, adminCookie, http.StatusOK)
	deleteJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/model-keys/"+createdModelKey.ID, adminCookie, http.StatusOK)

	auditResp := getJSONWithCookie(t, server.URL+"/api/admin/audit/logs?include_app_api=1&limit=20", adminCookie, http.StatusOK)
	if int(auditResp["count"].(float64)) == 0 {
		t.Fatalf("unexpected audit response: %#v", auditResp)
	}

}

func passwordHash(raw string) (string, error) {
	return password.Hash(raw)
}

func loginAndGetCookie(t *testing.T, baseURL string, identifier string, password string) *http.Cookie {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/auth/login", mustJSONBody(t, map[string]any{
		"identifier": identifier,
		"password":   password,
	}))
	if err != nil {
		t.Fatalf("new login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected login status=%d body=%s", resp.StatusCode, string(body))
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	return cookies[0]
}

func getJSONWithCookie(t *testing.T, url string, cookie *http.Cookie, wantStatus int) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	return decodeJSONBody(t, resp.Body, resp.StatusCode, wantStatus)
}

func getTextWithCookie(t *testing.T, url string, cookie *http.Cookie) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(bodyBytes)
}

func postJSONWithCookie(t *testing.T, url string, body map[string]any, cookie *http.Cookie, wantStatus int) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	return decodeJSONBody(t, resp.Body, resp.StatusCode, wantStatus)
}

func deleteJSONWithCookie(t *testing.T, url string, cookie *http.Cookie, wantStatus int) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete %s: %v", url, err)
	}
	defer resp.Body.Close()
	return decodeJSONBody(t, resp.Body, resp.StatusCode, wantStatus)
}

func putJSONWithCookie(t *testing.T, url string, body map[string]any, cookie *http.Cookie, wantStatus int) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put %s: %v", url, err)
	}
	defer resp.Body.Close()
	return decodeJSONBody(t, resp.Body, resp.StatusCode, wantStatus)
}
