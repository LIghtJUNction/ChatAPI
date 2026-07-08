package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/config"
	httpapi "github.com/zyf/chatapi/internal/http"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/repository/common"
	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	authaccess "github.com/zyf/chatapi/internal/service/auth/access"
	labauth "github.com/zyf/chatapi/internal/service/auth/authn/lab"
	appkey "github.com/zyf/chatapi/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf/chatapi/internal/service/auth/authz/modelkey"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	pendingsvc "github.com/zyf/chatapi/internal/service/chat/pending"
	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
	turnquerysvc "github.com/zyf/chatapi/internal/service/chat/turnquery"
)

func TestRouterLabModeAccessAndEndpoints(t *testing.T) {
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

	pending := pendingsvc.NewPendingRegistry()
	pending.Logger = logFactory.Layer(logging.LayerPending)
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
	modelKeyService := modelkey.NewService(st, "test-master-key")
	appKeyService := appkey.NewService(st)

	cfg := config.Default(config.ModeLab, "/tmp/chatapi-test")
	cfg.MetricsEnabled = true
	cfg.LabPassword = "secret"
	labService := labauth.NewService(cfg)

	server := httptest.NewServer(httpapi.NewRouter(httpapi.RouterDeps{
		Config:        cfg,
		ChatRepo:      st,
		AuthRepo:      st,
		ConfigRepo:    st,
		StorageRepo:   st,
		AuditRepo:     st,
		PlatformRepo:  st,
		Turn:          turnService,
		Query:         queryService,
		ModelAPIKeys:  modelKeyService,
		AppAPIKeys:    appKeyService,
		Lab:           labService,
		LoggerFactory: logFactory,
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/lab/workspace", nil)
	if err != nil {
		t.Fatalf("new lab workspace request: %v", err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("lab workspace request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected lab unauthorized status: %d", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected html password page, got %q", contentType)
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/lab/workspace?password=secret", nil)
	if err != nil {
		t.Fatalf("new lab bootstrap request: %v", err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("lab bootstrap request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unexpected lab bootstrap status: %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected lab access cookie")
	}

	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/lab/workspace", nil)
	if err != nil {
		t.Fatalf("new lab workspace request: %v", err)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("lab workspace with cookie: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected lab workspace status: %d", resp.StatusCode)
	}

	getJSONWithHeaders(t, server.URL+"/api/health", nil, http.StatusOK)
	getJSONWithHeaders(t, server.URL+"/api/ready", nil, http.StatusOK)
	getJSONWithHeaders(t, server.URL+"/api/setup/status", nil, http.StatusOK)
	status, body := getTextWithHeaders(t, server.URL+"/metrics", nil)
	if status != http.StatusOK || !strings.Contains(body, "chatapi_http_requests_total") {
		t.Fatalf("unexpected metrics response: status=%d body=%q", status, body)
	}
}

func TestRouterGlobalAccessRateLimit(t *testing.T) {
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

	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	cfg.MetricsEnabled = true
	cfg.AccessRateLimitRequests = 2
	cfg.AccessRateLimitWindow = time.Minute

	server := httptest.NewServer(httpapi.NewRouter(httpapi.RouterDeps{
		Config:        cfg,
		ChatRepo:      st,
		AuthRepo:      st,
		ConfigRepo:    st,
		StorageRepo:   st,
		AuditRepo:     st,
		PlatformRepo:  st,
		LoggerFactory: logFactory,
	}))
	defer server.Close()

	getJSONWithHeaders(t, server.URL+"/api/health", nil, http.StatusOK)
	getJSONWithHeaders(t, server.URL+"/api/health", nil, http.StatusOK)
	getJSONWithHeaders(t, server.URL+"/api/health", nil, http.StatusOK)

	getTextWithHeaders(t, server.URL+"/api/auth/session", nil)
	getTextWithHeaders(t, server.URL+"/api/auth/session", nil)
	status, body := getTextWithHeaders(t, server.URL+"/api/auth/session", nil)
	if status != http.StatusTooManyRequests || !strings.Contains(body, "request rate limited") {
		t.Fatalf("unexpected rate limit response: status=%d body=%q", status, body)
	}
}

func TestRouterPrincipalAccessRateLimitForAppKey(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:       "user_a",
		Username: "alice",
		Email:    "alice@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("new logger factory: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)

	appKeyService := appkey.NewService(st)
	_, rawAppKey, err := appKeyService.CreateKey(context.Background(), "user_a", "user-a-app", []string{"requests:read"}, nil, nil)
	if err != nil {
		t.Fatalf("create app key: %v", err)
	}
	queryService := &turnquerysvc.Service{Store: st, Logger: logFactory.Layer(logging.LayerTurnQuery)}
	accessSettings := authaccess.NewSettingsService(st, authaccess.Settings{})
	if _, err := accessSettings.Set(context.Background(), map[string]any{
		"app_key_rate_limit_requests": 1,
		"app_key_rate_limit_window":   "1m",
	}); err != nil {
		t.Fatalf("set access settings: %v", err)
	}

	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	server := httptest.NewServer(httpapi.NewRouter(httpapi.RouterDeps{
		Config:         cfg,
		ChatRepo:       st,
		AuthRepo:       st,
		ConfigRepo:     st,
		StorageRepo:    st,
		AuditRepo:      st,
		PlatformRepo:   st,
		Query:          queryService,
		AppAPIKeys:     appKeyService,
		Policy:         policy.NewService(),
		AccessSettings: accessSettings,
		LoggerFactory:  logFactory,
	}))
	defer server.Close()

	getJSONWithHeaders(t, server.URL+"/api/requests", map[string]string{
		"X-ChatAPI-App-Key": rawAppKey,
	}, http.StatusOK)

	status, body := getTextWithHeaders(t, server.URL+"/api/requests", map[string]string{
		"X-ChatAPI-App-Key": rawAppKey,
	})
	if status != http.StatusTooManyRequests || !strings.Contains(body, "principal rate limited") || !strings.Contains(body, "principal_rate_limited") {
		t.Fatalf("unexpected principal rate limit response: status=%d body=%q", status, body)
	}
}
