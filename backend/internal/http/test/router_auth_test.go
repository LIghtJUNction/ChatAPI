package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/platform/email"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/identity"
	localauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/local"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/verification"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	httpapp "github.com/zyf2007/ChatAPI/internal/testutil/httpapp"
)

type memorySender struct {
	messages []email.Message
}

func (s *memorySender) Send(_ context.Context, message email.Message) error {
	s.messages = append(s.messages, message)
	return nil
}

func (s *memorySender) lastCode(t *testing.T) string {
	t.Helper()
	if len(s.messages) == 0 {
		t.Fatal("expected at least one email message")
	}
	for _, field := range strings.Fields(s.messages[len(s.messages)-1].Text) {
		field = strings.Trim(field, " \t\r\n.,;:!?)(")
		if len(field) == 6 && strings.Trim(field, "0123456789") == "" {
			return field
		}
	}
	t.Fatalf("no 6-digit code found in email text: %q", s.messages[len(s.messages)-1].Text)
	return ""
}

func TestRouterLocalAuthFlow(t *testing.T) {
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

	sender := &memorySender{}
	sessionService, err := session.NewService(session.Config{Secret: "01234567890123456789012345678901"})
	if err != nil {
		t.Fatalf("new session service: %v", err)
	}
	verificationService := verification.NewService(st, sender)
	verificationService.Logger = logFactory.Layer(logging.LayerAuth)
	policies := policy.NewService()
	accountService := account.NewService(st)
	identityService := identity.NewService(accountService)
	localService := localauth.NewService(accountService, st, policies, sessionService, verificationService)
	localService.Logger = logFactory.Layer(logging.LayerAuth)
	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	cfg.SMTPEnabled = true
	server := httptest.NewServer(httpapp.MustNewRouter(httpapp.Input{
		Config:         cfg,
		MediaProcessor: testMediaProcessor(),
		ChatRepo:       st,
		AuthRepo:       st,
		ConfigRepo:     st,
		StorageRepo:    st,
		AuditRepo:      st,
		PlatformRepo:   st,
		LocalAuth:      localService,
		Verification:   verificationService,
		Policy:         policies,
		Accounts:       accountService,
		Identity:       identityService,
		UserSessions:   sessionService,
		LoggerFactory:  logFactory,
	}))
	defer server.Close()

	sessionResp := getJSONWithHeaders(t, server.URL+"/api/auth/session", nil, http.StatusOK)
	if authenticated, _ := sessionResp["authenticated"].(bool); authenticated {
		t.Fatalf("unexpected authenticated session response: %#v", sessionResp)
	}

	postJSONWithHeaders(t, server.URL+"/api/auth/verification/send", map[string]any{
		"email":   "alice@example.com",
		"purpose": "register",
	}, nil, http.StatusOK)
	registerCode := sender.lastCode(t)

	registerResp := postJSONWithHeaders(t, server.URL+"/api/auth/register", map[string]any{
		"username":          "alice",
		"email":             "alice@example.com",
		"password":          "pass-12345",
		"verification_code": registerCode,
	}, nil, http.StatusCreated)
	user := registerResp["user"].(map[string]any)
	if user["email"] != "alice@example.com" || user["username"] != "alice" {
		t.Fatalf("unexpected register response: %#v", registerResp)
	}

	loginReq, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login", mustJSONBody(t, map[string]any{
		"username": "alice",
		"password": "pass-12345",
	}))
	if err != nil {
		t.Fatalf("new login request: %v", err)
	}
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := http.DefaultClient.Do(loginReq)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected login status: %d", loginResp.StatusCode)
	}
	cookies := loginResp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie on login")
	}
	loginPayload := decodeJSONBody(t, loginResp.Body, loginResp.StatusCode, http.StatusOK)
	if loginPayload["ok"] != true {
		t.Fatalf("unexpected login payload: %#v", loginPayload)
	}

	sessionReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/session", nil)
	if err != nil {
		t.Fatalf("new session request: %v", err)
	}
	sessionReq.AddCookie(cookies[0])
	sessionRespRaw, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("session request: %v", err)
	}
	defer sessionRespRaw.Body.Close()
	sessionPayload := decodeJSONBody(t, sessionRespRaw.Body, sessionRespRaw.StatusCode, http.StatusOK)
	if authenticated, _ := sessionPayload["authenticated"].(bool); !authenticated {
		t.Fatalf("expected authenticated session: %#v", sessionPayload)
	}

	postJSONWithHeaders(t, server.URL+"/api/auth/password/forgot", map[string]any{
		"email": "alice@example.com",
	}, nil, http.StatusOK)
	resetCode := sender.lastCode(t)

	postJSONWithHeaders(t, server.URL+"/api/auth/password/reset", map[string]any{
		"email":             "alice@example.com",
		"verification_code": resetCode,
		"new_password":      "pass-67890",
	}, nil, http.StatusOK)

	status, body := postTextWithHeaders(t, server.URL+"/api/auth/login", map[string]any{
		"identifier": "alice@example.com",
		"password":   "pass-12345",
	}, nil)
	if status != http.StatusUnauthorized || !strings.Contains(body, "invalid credentials") {
		t.Fatalf("unexpected old password login result: status=%d body=%q", status, body)
	}

	postJSONWithHeaders(t, server.URL+"/api/auth/login", map[string]any{
		"identifier": "alice@example.com",
		"password":   "pass-67890",
	}, nil, http.StatusOK)
}

func mustJSONBody(t *testing.T, payload map[string]any) io.Reader {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return bytes.NewReader(raw)
}
