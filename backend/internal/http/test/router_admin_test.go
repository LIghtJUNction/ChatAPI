package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/platform/password"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	"github.com/zyf2007/ChatAPI/internal/service/admincontrol"
	auditsvc "github.com/zyf2007/ChatAPI/internal/service/audit"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/identity"
	localauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/local"
	authsettings "github.com/zyf2007/ChatAPI/internal/service/auth/authn/settings"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/verification"
	appkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	pendingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	httpapp "github.com/zyf2007/ChatAPI/internal/testutil/httpapp"
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
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:           "admin_user",
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: adminPasswordHash,
		Role:         "admin",
		IsActive:     true,
		LocalAdmin:   false,
	}); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID: "superadmin_user", Username: "root", Email: "root@example.com",
		PasswordHash: adminPasswordHash, Role: "admin", IsActive: true, LocalAdmin: true,
	}); err != nil {
		t.Fatalf("create superadmin user: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
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
	modelKeyService := modelkey.NewService(st, "test-master-key")
	appKeyService := appkey.NewService(st, "test-master-key")
	appKeyService.Logger = logFactory.Layer(logging.LayerAudit)
	pending := pendingsvc.NewPendingRegistry()
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
	adminControl := admincontrol.New(admincontrol.Deps{
		Accounts:     accountService,
		Query:        queryService,
		Turn:         turnService,
		ChatStore:    st,
		StorageStore: st,
		KeyStore:     st,
	})

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
	accessSettings := authaccess.NewSettingsService(st, authaccess.Settings{
		GlobalRateLimitRequests: cfg.AccessRateLimitRequests,
		GlobalRateLimitWindow:   cfg.AccessRateLimitWindow,
	})
	authSettings := authsettings.NewService(st, cfg)
	adminControl = admincontrol.New(admincontrol.Deps{
		Accounts:     accountService,
		Query:        queryService,
		Turn:         turnService,
		ChatStore:    st,
		StorageStore: st,
		KeyStore:     st,
	})
	server := httptest.NewServer(httpapp.MustNewRouter(httpapp.Input{
		Config:         cfg,
		MediaProcessor: testMediaProcessor(),
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
		AuthSettings:   authSettings,
		Accounts:       accountService,
		AdminControl:   adminControl,
		Audit:          auditService,
		Identity:       identityService,
		UserSessions:   sessionService,
		LoggerFactory:  logFactory,
	}))
	defer server.Close()

	status, _ := getTextWithHeaders(t, server.URL+"/api/admin/users", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized admin access, got %d", status)
	}
	status, _ = getTextWithHeaders(t, server.URL+"/api/admin/settings/overview", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized settings access, got %d", status)
	}
	status, _ = getTextWithHeaders(t, server.URL+"/api/admin/monitor/stream", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized monitoring access, got %d", status)
	}

	userCookie := loginAndGetCookie(t, server.URL, "bob@example.com", "user-pass")
	status, body := getTextWithCookie(t, server.URL+"/api/admin/users", userCookie)
	if status != http.StatusForbidden || !strings.Contains(body, "admin forbidden") {
		t.Fatalf("expected forbidden non-admin access, got status=%d body=%q", status, body)
	}
	status, _ = getTextWithCookie(t, server.URL+"/api/admin/settings/media", userCookie)
	if status != http.StatusForbidden {
		t.Fatalf("expected forbidden non-admin settings access, got %d", status)
	}

	adminCookie := loginAndGetCookie(t, server.URL, "admin@example.com", "admin-pass")
	roleDenied := putJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/role", map[string]any{"role": "admin"}, adminCookie, http.StatusForbidden)
	if !strings.Contains(roleDenied["error"].(string), "only the superadmin") {
		t.Fatalf("unexpected ordinary-admin role response: %#v", roleDenied)
	}
	superadminCookie := loginAndGetCookie(t, server.URL, "root@example.com", "admin-pass")
	roleUpdated := putJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/role", map[string]any{"role": "admin"}, superadminCookie, http.StatusOK)
	updatedUser, _ := roleUpdated["user"].(map[string]any)
	if updatedUser["role"] != "admin" {
		t.Fatalf("role was not promoted: %#v", roleUpdated)
	}
	putJSONWithCookie(t, server.URL+"/api/admin/users/normal_user/role", map[string]any{"role": "user"}, superadminCookie, http.StatusOK)
	monitorCtx, stopMonitor := context.WithCancel(context.Background())
	monitorReq, err := http.NewRequestWithContext(monitorCtx, http.MethodGet, server.URL+"/api/admin/monitor/stream?user_ids=normal_user", nil)
	if err != nil {
		t.Fatal(err)
	}
	monitorReq.AddCookie(adminCookie)
	monitorResp, err := http.DefaultClient.Do(monitorReq)
	if err != nil {
		t.Fatal(err)
	}
	monitorReader := bufio.NewReader(monitorResp.Body)
	eventLine, err := monitorReader.ReadString('\n')
	if err != nil || strings.TrimSpace(eventLine) != "event: monitor.snapshot" {
		t.Fatalf("unexpected monitoring event line %q: %v", eventLine, err)
	}
	dataLine, err := monitorReader.ReadString('\n')
	if err != nil || !strings.HasPrefix(dataLine, "data: ") {
		t.Fatalf("unexpected monitoring data line %q: %v", dataLine, err)
	}
	var monitorSnapshot map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(dataLine, "data: "))), &monitorSnapshot); err != nil {
		t.Fatal(err)
	}
	if monitorSnapshot["type"] != "monitor.snapshot" || monitorSnapshot["metrics"] == nil {
		t.Fatalf("unexpected monitoring snapshot: %#v", monitorSnapshot)
	}
	monitorUsers, _ := monitorSnapshot["user_connections"].(map[string]any)
	if len(monitorUsers) != 1 || monitorUsers["normal_user"] != float64(0) {
		t.Fatalf("monitoring snapshot was not filtered to requested users: %#v", monitorSnapshot)
	}
	stopMonitor()
	_ = monitorResp.Body.Close()

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
	if int(adminUsers["page"].(float64)) != 1 || int(adminUsers["page_size"].(float64)) != 10 || int(adminUsers["total"].(float64)) < 2 {
		t.Fatalf("unexpected admin users pagination: %#v", adminUsers)
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
	accessSettingsResp := getJSONWithCookie(t, server.URL+"/api/admin/settings/access", adminCookie, http.StatusOK)
	if accessSettingsResp["ok"] != true {
		t.Fatalf("unexpected access settings response: %#v", accessSettingsResp)
	}
	document, ok := accessSettingsResp["document"].(map[string]any)
	if !ok {
		t.Fatalf("expected settings document: %#v", accessSettingsResp)
	}
	current, ok := document["values"].(map[string]any)
	if !ok {
		t.Fatalf("expected current document in access settings response: %#v", accessSettingsResp)
	}
	if _, ok := current["global_rate_limit_requests"]; !ok {
		t.Fatalf("expected current global_rate_limit_requests: %#v", accessSettingsResp)
	}
	if _, ok := current["max_connections_per_instance"]; !ok {
		t.Fatalf("expected realtime connection limits in access settings: %#v", accessSettingsResp)
	}
	if _, ok := current["pending_turn_ttl"]; !ok {
		t.Fatalf("expected pending turn TTL in access settings: %#v", accessSettingsResp)
	}
	if _, ok := current["max_output_events_per_message"]; !ok {
		t.Fatalf("expected message event limit in access settings: %#v", accessSettingsResp)
	}
	accessSettingsResp = patchJSONWithCookie(t, server.URL+"/api/admin/settings/access", map[string]any{"values": map[string]any{
		"user_rate_limit_requests":              10,
		"user_rate_limit_window":                "1m",
		"app_key_rate_limit_requests":           20,
		"app_key_rate_limit_window":             "2m",
		"max_connections_per_user_per_instance": 6,
	}}, adminCookie, http.StatusOK)
	result, ok := accessSettingsResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected patch result: %#v", accessSettingsResp)
	}
	document, ok = result["document"].(map[string]any)
	if !ok {
		t.Fatalf("expected updated document: %#v", accessSettingsResp)
	}
	current, ok = document["values"].(map[string]any)
	if !ok {
		t.Fatalf("expected current document after update: %#v", accessSettingsResp)
	}
	if int(current["user_rate_limit_requests"].(float64)) != 10 {
		t.Fatalf("unexpected saved access settings response: %#v", accessSettingsResp)
	}
	if int(current["max_connections_per_user_per_instance"].(float64)) != 6 {
		t.Fatalf("unexpected saved realtime settings response: %#v", accessSettingsResp)
	}
	lastWrite := patchJSONWithCookie(t, server.URL+"/api/admin/settings/access", map[string]any{"values": map[string]any{"user_rate_limit_requests": 11}}, adminCookie, http.StatusOK)
	lastWriteResult, _ := lastWrite["result"].(map[string]any)
	lastWriteDocument, _ := lastWriteResult["document"].(map[string]any)
	lastWriteValues, _ := lastWriteDocument["values"].(map[string]any)
	if int(lastWriteValues["user_rate_limit_requests"].(float64)) != 11 {
		t.Fatalf("last submitted settings were not persisted: %#v", lastWrite)
	}
	emptyPatch := patchJSONWithCookie(t, server.URL+"/api/admin/settings/access", map[string]any{"values": map[string]any{}}, adminCookie, http.StatusBadRequest)
	if emptyPatch["ok"] != false {
		t.Fatalf("expected empty patch rejection: %#v", emptyPatch)
	}

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
	setSameOriginHeader(req)
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
	setSameOriginHeader(req)
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
	setSameOriginHeader(req)
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
	setSameOriginHeader(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	return decodeJSONBody(t, resp.Body, resp.StatusCode, wantStatus)
}

func patchJSONWithCookie(t *testing.T, url string, body map[string]any, cookie *http.Cookie, wantStatus int) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, url, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	setSameOriginHeader(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch %s: %v", url, err)
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
	setSameOriginHeader(req)
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
	setSameOriginHeader(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put %s: %v", url, err)
	}
	defer resp.Body.Close()
	return decodeJSONBody(t, resp.Body, resp.StatusCode, wantStatus)
}

func setSameOriginHeader(req *http.Request) {
	if req == nil || req.URL == nil {
		return
	}
	req.Header.Set("Origin", (&url.URL{Scheme: req.URL.Scheme, Host: req.URL.Host}).String())
}
