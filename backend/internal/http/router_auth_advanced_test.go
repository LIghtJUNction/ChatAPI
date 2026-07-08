package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/pquerna/otp/totp"
	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/config"
	httpapi "github.com/zyf/chatapi/internal/http"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/platform/email"
	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/service/account"
	auditsvc "github.com/zyf/chatapi/internal/service/audit"
	authadmin "github.com/zyf/chatapi/internal/service/auth/admin"
	"github.com/zyf/chatapi/internal/service/auth/authn/geetest"
	"github.com/zyf/chatapi/internal/service/auth/authn/identity"
	localauth "github.com/zyf/chatapi/internal/service/auth/authn/local"
	oidcsvc "github.com/zyf/chatapi/internal/service/auth/authn/oidc"
	"github.com/zyf/chatapi/internal/service/auth/authn/ratelimit"
	authsettings "github.com/zyf/chatapi/internal/service/auth/authn/settings"
	totpsvc "github.com/zyf/chatapi/internal/service/auth/authn/totp"
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

func TestAuthSettingsGeeTestAndTOTPFlow(t *testing.T) {
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

	adminPasswordHash, _ := passwordHash("admin-pass")
	userPasswordHash, _ := passwordHash("alice-pass")
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
		ID:           "alice_user",
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: userPasswordHash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create alice user: %v", err)
	}

	geetestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "success"})
	}))
	defer geetestServer.Close()

	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	cfg.SMTPEnabled = true
	cfg.GeetestCaptchaID = "captcha-id"
	cfg.GeetestCaptchaKey = "captcha-key"
	cfg.GeetestAPIServer = geetestServer.URL
	cfg.MasterKey = "01234567890123456789012345678901"

	server, sender := newAdvancedAuthServer(t, st, cfg, logFactory)
	defer server.Close()

	adminCookie := loginAndGetCookie(t, server.URL, "admin@example.com", "admin-pass")
	postJSONWithCookie(t, server.URL+"/api/admin/auth/settings", map[string]any{
		"external_registration_enabled":                 true,
		"email_verification_enabled":                    true,
		"password_reset_enabled":                        true,
		"local_password_login_enabled":                  false,
		"geetest_login_enabled":                         true,
		"geetest_register_enabled":                      true,
		"geetest_password_reset_enabled":                true,
		"registration_email_domain_restriction_enabled": false,
	}, adminCookie, http.StatusOK)

	status, body := postTextWithHeaders(t, server.URL+"/api/auth/login", map[string]any{
		"identifier": "alice@example.com",
		"password":   "alice-pass",
	}, nil)
	if status != http.StatusForbidden || !strings.Contains(body, "local password login is disabled") {
		t.Fatalf("unexpected disabled login result: status=%d body=%q", status, body)
	}

	postJSONWithCookie(t, server.URL+"/api/admin/auth/settings", map[string]any{
		"external_registration_enabled":                 true,
		"email_verification_enabled":                    true,
		"password_reset_enabled":                        true,
		"local_password_login_enabled":                  true,
		"geetest_login_enabled":                         true,
		"geetest_register_enabled":                      true,
		"geetest_password_reset_enabled":                true,
		"registration_email_domain_restriction_enabled": false,
	}, adminCookie, http.StatusOK)

	params := map[string]any{
		"lot_number":     "lot",
		"captcha_output": "output",
		"pass_token":     "token",
		"gen_time":       "time",
	}
	postJSONWithHeaders(t, server.URL+"/api/auth/register/send-code", map[string]any{
		"email":          "bob@example.com",
		"geetest_params": params,
	}, nil, http.StatusOK)
	registerCode := sender.lastCode(t)
	postJSONWithHeaders(t, server.URL+"/api/auth/register", map[string]any{
		"email":             "bob@example.com",
		"password":          "bob-pass",
		"verification_code": registerCode,
		"geetest_params":    params,
	}, nil, http.StatusCreated)

	status, body = postTextWithHeaders(t, server.URL+"/api/auth/login", map[string]any{
		"identifier": "alice@example.com",
		"password":   "alice-pass",
	}, nil)
	if status != http.StatusBadRequest || !strings.Contains(body, "geetest verification is required") {
		t.Fatalf("unexpected geetest login result: status=%d body=%q", status, body)
	}

	loginReq, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login", mustJSONBody(t, map[string]any{
		"identifier":     "alice@example.com",
		"password":       "alice-pass",
		"geetest_params": params,
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
	aliceCookie := loginResp.Cookies()[0]

	setupResp := getJSONWithCookie(t, server.URL+"/api/auth/totp/setup", aliceCookie, http.StatusOK)
	secret := setupResp["secret"].(string)
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate totp code: %v", err)
	}
	postJSONWithCookie(t, server.URL+"/api/auth/totp/confirm", map[string]any{
		"secret": secret,
		"code":   code,
	}, aliceCookie, http.StatusOK)
	postJSONWithCookie(t, server.URL+"/api/auth/logout", map[string]any{}, aliceCookie, http.StatusOK)

	status, body = postTextWithHeaders(t, server.URL+"/api/auth/login", map[string]any{
		"identifier":     "alice@example.com",
		"password":       "alice-pass",
		"geetest_params": params,
	}, nil)
	if status != http.StatusUnauthorized || !strings.Contains(body, "totp_required") {
		t.Fatalf("unexpected totp challenge result: status=%d body=%q", status, body)
	}

	code, err = totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate second totp code: %v", err)
	}
	postJSONWithHeaders(t, server.URL+"/api/auth/login", map[string]any{
		"identifier":     "alice@example.com",
		"password":       "alice-pass",
		"totp":           code,
		"geetest_params": params,
	}, nil, http.StatusOK)
}

func TestOIDCAdminEmailFlow(t *testing.T) {
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

	provider := newTestOIDCProvider(t, testOIDCProviderConfig{
		Email:         "admin@example.com",
		EmailVerified: true,
		Subject:       "oidc-admin-sub",
		Name:          "OIDC Admin",
		PreferredName: "oidc-admin",
	})
	defer provider.Close()

	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	cfg.MasterKey = "01234567890123456789012345678901"
	cfg.OIDCEnabled = true
	cfg.OIDCIssuerURL = provider.Issuer()
	cfg.OIDCClientID = "chatapi"
	cfg.OIDCClientSecret = "secret"
	cfg.OIDCRedirectURL = "http://chat.example.com/api/auth/oidc/callback"
	cfg.OIDCAutoCreateUser = true
	cfg.OIDCAdminEmails = []string{"admin@example.com"}

	server, _ := newAdvancedAuthServerWithConfig(t, st, cfg, logFactory, func(baseURL string, cfg *config.Config) {
		cfg.OIDCRedirectURL = baseURL + "/api/auth/oidc/callback"
	})
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(server.URL + "/api/auth/oidc/login")
	if err != nil {
		t.Fatalf("oidc login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("unexpected oidc login status: %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	resp, err = client.Get(location)
	if err != nil {
		t.Fatalf("provider authorize: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("unexpected provider authorize status: %d", resp.StatusCode)
	}
	callbackURL := resp.Header.Get("Location")
	resp, err = client.Get(callbackURL)
	if err != nil {
		t.Fatalf("oidc callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected oidc callback status: %d body=%q callback=%q", resp.StatusCode, string(body), callbackURL)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode oidc callback payload: %v", err)
	}
	user := payload["user"].(map[string]any)
	if user["role"] != "admin" {
		t.Fatalf("unexpected oidc role payload: %#v", payload)
	}

	resp, err = client.Get(server.URL + "/api/auth/session")
	if err != nil {
		t.Fatalf("session request: %v", err)
	}
	defer resp.Body.Close()
	var sessionPayload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sessionPayload); err != nil {
		t.Fatalf("decode session payload: %v", err)
	}
	sessionUser := sessionPayload["user"].(map[string]any)
	if sessionUser["role"] != "admin" {
		t.Fatalf("unexpected session role: %#v", sessionPayload)
	}
	items, err := st.ListUserIdentities(context.Background(), sessionUser["id"].(string))
	if err != nil || len(items) != 1 || items[0].Provider != "oidc" {
		t.Fatalf("unexpected oidc identities: items=%#v err=%v", items, err)
	}
}

func newAdvancedAuthServer(t *testing.T, st *sqlitestore.Store, cfg config.Config, logFactory *logging.Factory) (*httptest.Server, *memorySender) {
	t.Helper()
	sender := &memorySender{}
	server, sender := newAdvancedAuthServerWithConfig(t, st, cfg, logFactory, nil)
	return server, sender
}

func newAdvancedAuthServerWithConfig(t *testing.T, st *sqlitestore.Store, cfg config.Config, logFactory *logging.Factory, mutate func(baseURL string, cfg *config.Config)) (*httptest.Server, *memorySender) {
	t.Helper()
	sender := &memorySender{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test server: %v", err)
	}
	baseURL := "http://" + listener.Addr().String()
	if mutate != nil {
		mutate(baseURL, &cfg)
	}
	server := httptest.NewUnstartedServer(httpapi.NewRouter(newAdvancedRouterDeps(st, cfg, logFactory, sender)))
	server.Listener = listener
	server.Start()
	return server, sender
}

func newAdvancedRouterDeps(st *sqlitestore.Store, cfg config.Config, logFactory *logging.Factory, sender email.Sender) httpapi.RouterDeps {
	policies := policy.NewService()
	sessionService, err := session.NewService(session.Config{Secret: "01234567890123456789012345678901"})
	if err != nil {
		panic(err)
	}
	verificationService := verification.NewService(st, sender)
	verificationService.Logger = logFactory.Layer(logging.LayerAuth)
	accountService := account.NewService(st)
	localService := localauth.NewService(accountService, st, policies, sessionService, verificationService)
	localService.Logger = logFactory.Layer(logging.LayerAuth)
	identityService := identity.NewService(accountService)
	modelKeyService := modelkey.NewService(st, cfg.MasterKey)
	appKeyService := appkey.NewService(st)
	appKeyService.Logger = logFactory.Layer(logging.LayerAudit)
	userService := usersvc.NewService(accountService, st, appKeyService, modelKeyService)
	authSettings := authsettings.NewService(st, cfg)
	geetestService := geetest.NewService(cfg, nil)
	totpService := totpsvc.NewService(st, cfg.MasterKey, "ChatAPI")
	oidcService := oidcsvc.NewService(accountService, cfg)
	loginLimiter := ratelimit.NewService(5, time.Minute)
	auditService := auditsvc.NewService(st)

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

	return httpapi.RouterDeps{
		Config:        cfg,
		Turn:          turnService,
		Query:         queryService,
		ModelAPIKeys:  modelKeyService,
		AppAPIKeys:    appKeyService,
		LocalAuth:     localService,
		Verification:  verificationService,
		Policy:        policies,
		AuthSettings:  authSettings,
		GeeTest:       geetestService,
		TOTP:          totpService,
		OIDC:          oidcService,
		LoginLimiter:  loginLimiter,
		AdminUsers:    authadmin.NewService(accountService, st, policies),
		AdminChat:     chatadmin.NewService(queryService, turnService, st),
		Audit:         auditService,
		Identity:      identityService,
		Users:         userService,
		UserSessions:  sessionService,
		LoggerFactory: logFactory,
	}
}

type testOIDCProviderConfig struct {
	Email         string
	EmailVerified bool
	Subject       string
	Name          string
	PreferredName string
}

type testOIDCProvider struct {
	server     *httptest.Server
	issuer     string
	privateKey *rsa.PrivateKey
	keyID      string
	clientID   string
	claims     testOIDCProviderConfig
	codeNonce  map[string]string
}

func newTestOIDCProvider(t *testing.T, cfg testOIDCProviderConfig) *testOIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	p := &testOIDCProvider{
		privateKey: key,
		keyID:      "test-key",
		clientID:   "chatapi",
		claims:     cfg,
		codeNonce:  map[string]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", p.handleDiscovery)
	mux.HandleFunc("/authorize", p.handleAuthorize)
	mux.HandleFunc("/token", p.handleToken)
	mux.HandleFunc("/userinfo", p.handleUserInfo)
	mux.HandleFunc("/jwks", p.handleJWKS)
	p.server = httptest.NewServer(mux)
	p.issuer = p.server.URL
	return p
}

func (p *testOIDCProvider) Close()         { p.server.Close() }
func (p *testOIDCProvider) Issuer() string { return p.issuer }

func (p *testOIDCProvider) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                p.issuer,
		"authorization_endpoint":                p.issuer + "/authorize",
		"token_endpoint":                        p.issuer + "/token",
		"userinfo_endpoint":                     p.issuer + "/userinfo",
		"jwks_uri":                              p.issuer + "/jwks",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (p *testOIDCProvider) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	nonce := strings.TrimSpace(r.URL.Query().Get("nonce"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	redirectURI := strings.TrimSpace(r.URL.Query().Get("redirect_uri"))
	code := "code-" + nonce
	p.codeNonce[code] = nonce
	http.Redirect(w, r, redirectURI+"?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), http.StatusFound)
}

func (p *testOIDCProvider) handleToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	code := strings.TrimSpace(r.Form.Get("code"))
	nonce := p.codeNonce[code]
	idToken, err := p.signedIDToken(nonce)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

func (p *testOIDCProvider) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sub":                p.claims.Subject,
		"email":              p.claims.Email,
		"email_verified":     p.claims.EmailVerified,
		"name":               p.claims.Name,
		"preferred_username": p.claims.PreferredName,
	})
}

func (p *testOIDCProvider) handleJWKS(w http.ResponseWriter, r *http.Request) {
	jwk := jose.JSONWebKey{Key: &p.privateKey.PublicKey, KeyID: p.keyID, Algorithm: string(jose.RS256), Use: "sig"}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keys": []any{jwk.Public()},
	})
}

func (p *testOIDCProvider) signedIDToken(nonce string) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.privateKey}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}
	now := time.Now()
	return jwt.Signed(signer).Claims(jwt.Claims{
		Issuer:   p.issuer,
		Subject:  p.claims.Subject,
		Audience: jwt.Audience{p.clientID},
		IssuedAt: jwt.NewNumericDate(now),
		Expiry:   jwt.NewNumericDate(now.Add(time.Hour)),
	}).Claims(map[string]any{
		"nonce":              nonce,
		"email":              p.claims.Email,
		"email_verified":     p.claims.EmailVerified,
		"name":               p.claims.Name,
		"preferred_username": p.claims.PreferredName,
	}).Serialize()
}
