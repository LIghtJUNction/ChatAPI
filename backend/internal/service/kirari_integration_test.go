package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/platform/secretbox"
	"github.com/zyf/chatapi/internal/repository/migrations"
	"github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/store"
)

func TestKirariIntegrationServiceChatCompletionsRaw(t *testing.T) {
	provider := newKirariServiceTestProvider(t)
	defer provider.Close()

	dataStore := newKirariIntegrationTestStore(t)
	svc := NewKirariIntegrationService(dataStore, config.Config{
		MasterKey:                        "test-master-key",
		KirariEnabled:                    true,
		KirariIssuerURL:                  provider.Issuer(),
		KirariClientID:                   "chatapi",
		KirariClientSecret:               "secret",
		KirariRedirectURL:                "https://chat.example.com/api/integrations/kirari/callback",
		KirariScopes:                     []string{"openid", "profile", "email", "offline_access", "llm:read", "llm:stream"},
		KirariAllowedIssuers:             []string{provider.Issuer()},
		KirariMetaEndpointURL:            provider.Issuer() + "/api/llm/meta",
		KirariChatCompletionsEndpointURL: provider.Issuer() + "/api/llm/chat/completions",
	}, provider.server.Client())
	svc.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

	storeKirariConnectionForTest(t, dataStore, svc, "user_kirari", kirariStoredConnection{
		IssuerURL:              provider.Issuer(),
		Subject:                "kirari-test-sub",
		AccessTokenCiphertext:  mustSealForTest(t, "expired-access-token", "test-master-key"),
		RefreshTokenCiphertext: mustSealForTest(t, "refresh-token", "test-master-key"),
		ExpiresAt:              timePtr(time.Unix(1_700_000_000, 0).UTC().Add(-time.Minute)),
		GrantedScopes:          []string{"openid", "llm:read", "llm:stream"},
	})

	resp, err := svc.ChatCompletionsRaw(context.Background(), "user_kirari", map[string]any{
		"model":  "kirari-model",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "hello stream"},
		},
	})
	if err != nil {
		t.Fatalf("chat completions raw: %v", err)
	}
	defer resp.Body.Close()
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		t.Fatalf("expected event-stream content type, got %q", resp.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream body: %v", err)
	}
	if !strings.Contains(string(body), "data: ") || !strings.Contains(string(body), "[DONE]") {
		t.Fatalf("unexpected stream body: %s", string(body))
	}
	if provider.lastChatAuthorization != "Bearer refreshed-access-token" {
		t.Fatalf("unexpected chat authorization header: %q", provider.lastChatAuthorization)
	}

	updated, err := dataStore.GetUserConfig(context.Background(), "user_kirari", kirariUserConfigKey)
	if err != nil {
		t.Fatalf("load updated kirari config: %v", err)
	}
	record, err := decodeKirariStoredConnection(updated.Value)
	if err != nil {
		t.Fatalf("decode updated kirari config: %v", err)
	}
	accessToken, err := secretbox.Open(record.AccessTokenCiphertext, "test-master-key")
	if err != nil {
		t.Fatalf("open refreshed access token: %v", err)
	}
	if accessToken != "refreshed-access-token" {
		t.Fatalf("unexpected refreshed access token: %q", accessToken)
	}
}

func newKirariIntegrationTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	st, err := sqlite.Open(t.TempDir() + "/chatapi.sqlite3")
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite store: %v", err)
	}
	return st
}

func storeKirariConnectionForTest(t *testing.T, dataStore store.Store, svc *KirariIntegrationService, userID string, connection kirariStoredConnection) {
	t.Helper()
	if _, err := dataStore.CreateUser(context.Background(), store.CreateUserInput{
		ID:       userID,
		Username: userID,
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := dataStore.SetUserConfig(context.Background(), store.SetUserConfigInput{
		UserID: userID,
		Key:    kirariUserConfigKey,
		Value:  connection.toMap(),
	}); err != nil {
		t.Fatalf("set kirari config: %v", err)
	}
}

func mustSealForTest(t *testing.T, value string, key string) string {
	t.Helper()
	sealed, err := secretbox.Seal(value, key)
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	return sealed
}

func timePtr(value time.Time) *time.Time {
	v := value.UTC()
	return &v
}

type kirariServiceTestProvider struct {
	server                *httptest.Server
	privateKey            *rsa.PrivateKey
	keyID                 string
	lastChatAuthorization string
}

func newKirariServiceTestProvider(t *testing.T) *kirariServiceTestProvider {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	provider := &kirariServiceTestProvider{
		privateKey: privateKey,
		keyID:      "kirari-service-test-key",
	}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                        provider.server.URL,
				"authorization_endpoint":        provider.server.URL + "/authorize",
				"token_endpoint":                provider.server.URL + "/token",
				"jwks_uri":                      provider.server.URL + "/jwks",
				"userinfo_endpoint":             provider.server.URL + "/userinfo",
				"llm_meta_endpoint":             provider.server.URL + "/api/llm/meta",
				"llm_chat_completions_endpoint": provider.server.URL + "/api/llm/chat/completions",
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]any{{
					"kty": "RSA",
					"alg": "RS256",
					"use": "sig",
					"kid": provider.keyID,
					"n":   base64.RawURLEncoding.EncodeToString(provider.privateKey.PublicKey.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(bigIntBytes(provider.privateKey.PublicKey.E)),
				}},
			})
		case "/token":
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if r.Form.Get("grant_type") != "refresh_token" {
				http.Error(w, "unsupported grant type", http.StatusBadRequest)
				return
			}
			idToken, err := provider.signedIDToken()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "refreshed-access-token",
				"refresh_token": "refresh-token-2",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"scope":         "openid profile email offline_access llm:read llm:stream",
				"id_token":      idToken,
			})
		case "/api/llm/chat/completions":
			provider.lastChatAuthorization = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"id\":\"chunk-1\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sub":                "kirari-test-sub",
				"email":              "kirari@example.com",
				"email_verified":     true,
				"name":               "Kirari User",
				"preferred_username": "kirari-user",
			})
		case "/api/llm/meta":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{{"id": "kirari-model"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	return provider
}

func (p *kirariServiceTestProvider) Close() {
	if p != nil && p.server != nil {
		p.server.Close()
	}
}

func (p *kirariServiceTestProvider) Issuer() string {
	if p == nil || p.server == nil {
		return ""
	}
	return p.server.URL
}

func (p *kirariServiceTestProvider) signedIDToken() (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.RS256,
		Key: jose.JSONWebKey{
			Key:       p.privateKey,
			KeyID:     p.keyID,
			Use:       "sig",
			Algorithm: string(jose.RS256),
		},
	}, nil)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	return jwt.Signed(signer).Claims(map[string]any{
		"iss":                p.server.URL,
		"sub":                "kirari-test-sub",
		"aud":                "chatapi",
		"iat":                now.Unix(),
		"exp":                now.Add(time.Hour).Unix(),
		"email":              "kirari@example.com",
		"email_verified":     true,
		"name":               "Kirari User",
		"preferred_username": "kirari-user",
		"nonce":              "unused-refresh-token-flow",
	}).Serialize()
}

func bigIntBytes(value int) []byte {
	if value == 0 {
		return []byte{0}
	}
	out := make([]byte, 0, 8)
	for value > 0 {
		out = append([]byte{byte(value & 0xff)}, out...)
		value >>= 8
	}
	return out
}
