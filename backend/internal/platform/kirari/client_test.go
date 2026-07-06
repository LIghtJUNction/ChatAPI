package kirari

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestClientAuthorizationURLAndTokenFlow(t *testing.T) {
	provider := newTestOIDCProvider(t, testOIDCProviderConfig{
		Subject:           "kirari-subject",
		Email:             "kirari@example.com",
		EmailVerified:     true,
		Name:              "Kirari User",
		PreferredUsername: "kirari-user",
	})
	defer provider.Close()

	client, err := NewClient(Config{
		IssuerURL:      provider.Issuer(),
		ClientID:       "chatapi",
		ClientSecret:   "secret",
		RedirectURL:    "https://chat.example.com/api/integrations/kirari/callback",
		AllowedIssuers: []string{provider.Issuer()},
	}, provider.server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	authURL, session, err := client.AuthorizationURL(context.Background(), AuthorizationOptions{
		Prompt:    "consent",
		LoginHint: "kirari@example.com",
	})
	if err != nil {
		t.Fatalf("authorization url: %v", err)
	}
	if session.State == "" || session.Nonce == "" || session.CodeVerifier == "" || session.CodeChallengeMethod != "S256" {
		t.Fatalf("unexpected authorization session: %#v", session)
	}
	parsed, err := neturl.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	values := parsed.Query()
	if !strings.HasPrefix(authURL, provider.Issuer()+"/authorize?") ||
		values.Get("client_id") != "chatapi" ||
		values.Get("redirect_uri") != "https://chat.example.com/api/integrations/kirari/callback" ||
		values.Get("nonce") != session.Nonce ||
		values.Get("code_challenge") != session.CodeChallenge ||
		values.Get("code_challenge_method") != "S256" ||
		values.Get("prompt") != "consent" ||
		values.Get("login_hint") != "kirari@example.com" {
		t.Fatalf("unexpected auth url: %s", authURL)
	}

	result, err := client.ExchangeCode(context.Background(), "auth-code", session)
	if err != nil {
		t.Fatalf("exchange code: %v", err)
	}
	if result.Identity.Subject != "kirari-subject" ||
		result.Identity.Email != "kirari@example.com" ||
		!result.Identity.EmailVerified ||
		result.Identity.PreferredUsername != "kirari-user" ||
		result.TokenSet.AccessToken != "access-auth-code" ||
		result.TokenSet.RefreshToken != "refresh-auth-code" ||
		result.TokenSet.IDToken == "" {
		t.Fatalf("unexpected token exchange result: %#v", result)
	}

	refreshed, err := client.RefreshToken(context.Background(), result.TokenSet.RefreshToken)
	if err != nil {
		t.Fatalf("refresh token: %v", err)
	}
	if refreshed.AccessToken != "access-refreshed" || refreshed.RefreshToken != "refresh-refreshed" {
		t.Fatalf("unexpected refreshed token set: %#v", refreshed)
	}

	userInfo, err := client.UserInfo(context.Background(), refreshed.AccessToken)
	if err != nil {
		t.Fatalf("userinfo: %v", err)
	}
	if userInfo.Subject != "kirari-subject" || userInfo.Email != "kirari@example.com" {
		t.Fatalf("unexpected userinfo: %#v", userInfo)
	}

	meta, err := client.Meta(context.Background(), refreshed.AccessToken)
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if nestedString(meta, "models", "0", "id") != "kirari-model" {
		t.Fatalf("unexpected meta response: %#v", meta)
	}

	resp, err := client.ChatCompletions(context.Background(), refreshed.AccessToken, map[string]any{
		"model": "kirari-model",
		"messages": []map[string]any{
			{"role": "user", "content": "hello kirari"},
		},
	})
	if err != nil {
		t.Fatalf("chat completions: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read chat completions body: %v", err)
	}
	if !strings.Contains(string(body), `"model":"kirari-model"`) || !strings.Contains(string(body), `"hello kirari"`) {
		t.Fatalf("unexpected chat completions body: %s", string(body))
	}

	if provider.lastAuthHeader("meta") != "Bearer access-refreshed" || provider.lastAuthHeader("chat") != "Bearer access-refreshed" {
		t.Fatalf("unexpected bearer propagation: meta=%q chat=%q", provider.lastAuthHeader("meta"), provider.lastAuthHeader("chat"))
	}
}

func TestClientDiscoverRejectsUnexpectedIssuer(t *testing.T) {
	provider := newTestOIDCProvider(t, testOIDCProviderConfig{
		Subject: "kirari-subject",
	})
	defer provider.Close()

	client, err := NewClient(Config{
		IssuerURL:      provider.Issuer(),
		ClientID:       "chatapi",
		ClientSecret:   "secret",
		RedirectURL:    "https://chat.example.com/callback",
		AllowedIssuers: []string{"https://other.example.com"},
	}, provider.server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if _, err := client.Discover(context.Background()); err != ErrIssuerNotAllowed {
		t.Fatalf("expected issuer allowlist rejection, got %v", err)
	}
}

func TestClientExchangeCodeRejectsUserInfoSubjectMismatch(t *testing.T) {
	provider := newTestOIDCProvider(t, testOIDCProviderConfig{
		Subject:           "kirari-subject",
		UserInfoSubject:   "other-subject",
		Email:             "kirari@example.com",
		EmailVerified:     true,
		PreferredUsername: "kirari-user",
	})
	defer provider.Close()

	client, err := NewClient(Config{
		IssuerURL:      provider.Issuer(),
		ClientID:       "chatapi",
		ClientSecret:   "secret",
		RedirectURL:    "https://chat.example.com/callback",
		AllowedIssuers: []string{provider.Issuer()},
	}, provider.server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, session, err := client.AuthorizationURL(context.Background(), AuthorizationOptions{})
	if err != nil {
		t.Fatalf("authorization url: %v", err)
	}
	if _, err := client.ExchangeCode(context.Background(), "auth-code", session); err != ErrSubjectMismatch {
		t.Fatalf("expected subject mismatch, got %v", err)
	}
}

type testOIDCProviderConfig struct {
	Subject           string
	UserInfoSubject   string
	Email             string
	EmailVerified     bool
	Name              string
	PreferredUsername string
}

type testOIDCProvider struct {
	server      *httptest.Server
	issuer      string
	privateKey  *rsa.PrivateKey
	keyID       string
	config      testOIDCProviderConfig
	lastHeaders map[string]string
}

func newTestOIDCProvider(t *testing.T, cfg testOIDCProviderConfig) *testOIDCProvider {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	provider := &testOIDCProvider{
		privateKey:  privateKey,
		keyID:       "test-kid",
		config:      cfg,
		lastHeaders: map[string]string{},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                        provider.issuer,
				"authorization_endpoint":        provider.issuer + "/authorize",
				"token_endpoint":                provider.issuer + "/token",
				"jwks_uri":                      provider.issuer + "/jwks",
				"userinfo_endpoint":             provider.issuer + "/userinfo",
				"llm_meta_endpoint":             provider.issuer + "/api/llm/meta",
				"llm_chat_completions_endpoint": provider.issuer + "/api/llm/chat/completions",
				"llm_supported_scopes":          []string{"llm:read", "llm:stream"},
			})
		case "/jwks":
			jwk := jose.JSONWebKey{
				Key:       &provider.privateKey.PublicKey,
				KeyID:     provider.keyID,
				Algorithm: string(jose.RS256),
				Use:       "sig",
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []jose.JSONWebKey{jwk}})
		case "/token":
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			grantType := r.Form.Get("grant_type")
			var accessToken string
			var refreshToken string
			if grantType == "authorization_code" {
				if r.Form.Get("code_verifier") == "" {
					http.Error(w, "missing code_verifier", http.StatusBadRequest)
					return
				}
				accessToken = "access-auth-code"
				refreshToken = "refresh-auth-code"
			} else if grantType == "refresh_token" {
				if r.Form.Get("refresh_token") != "refresh-auth-code" {
					http.Error(w, "unexpected refresh_token", http.StatusBadRequest)
					return
				}
				accessToken = "access-refreshed"
				refreshToken = "refresh-refreshed"
			} else {
				http.Error(w, "unsupported grant_type", http.StatusBadRequest)
				return
			}
			idToken, err := provider.signIDToken()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  accessToken,
				"refresh_token": refreshToken,
				"token_type":    "Bearer",
				"expires_in":    3600,
				"scope":         "openid profile email offline_access llm:read llm:stream",
				"id_token":      idToken,
			})
		case "/userinfo":
			provider.lastHeaders["userinfo"] = r.Header.Get("Authorization")
			subject := cfg.Subject
			if strings.TrimSpace(cfg.UserInfoSubject) != "" {
				subject = cfg.UserInfoSubject
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sub":                subject,
				"email":              cfg.Email,
				"email_verified":     cfg.EmailVerified,
				"name":               cfg.Name,
				"preferred_username": cfg.PreferredUsername,
			})
		case "/api/llm/meta":
			provider.lastHeaders["meta"] = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"id": "kirari-model", "available": true, "price": map[string]any{"input": 1.23}},
				},
			})
		case "/api/llm/chat/completions":
			provider.lastHeaders["chat"] = r.Header.Get("Authorization")
			body, _ := io.ReadAll(r.Body)
			defer r.Body.Close()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"echo":` + string(body) + `}`))
		default:
			http.NotFound(w, r)
		}
	})
	provider.server = httptest.NewServer(handler)
	provider.issuer = provider.server.URL
	return provider
}

func (p *testOIDCProvider) Close() {
	if p == nil || p.server == nil {
		return
	}
	p.server.Close()
}

func (p *testOIDCProvider) Issuer() string {
	if p == nil {
		return ""
	}
	return p.issuer
}

func (p *testOIDCProvider) lastAuthHeader(key string) string {
	if p == nil {
		return ""
	}
	return p.lastHeaders[key]
}

func (p *testOIDCProvider) signIDToken() (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.RS256,
		Key: jose.JSONWebKey{
			Key:       p.privateKey,
			KeyID:     p.keyID,
			Algorithm: string(jose.RS256),
			Use:       "sig",
		},
	}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	return jwt.Signed(signer).Claims(map[string]any{
		"iss":                p.issuer,
		"sub":                p.config.Subject,
		"aud":                "chatapi",
		"exp":                now.Add(time.Hour).Unix(),
		"iat":                now.Unix(),
		"email":              p.config.Email,
		"email_verified":     p.config.EmailVerified,
		"name":               p.config.Name,
		"preferred_username": p.config.PreferredUsername,
	}).Serialize()
}

func nestedString(record map[string]any, path ...string) string {
	current := any(record)
	for _, key := range path {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[key]
		case []any:
			index := 0
			for _, ch := range key {
				index = index*10 + int(ch-'0')
			}
			if index < 0 || index >= len(typed) {
				return ""
			}
			current = typed[index]
		default:
			return ""
		}
	}
	text, _ := current.(string)
	return text
}
