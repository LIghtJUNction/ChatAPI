package kirari

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ErrInvalidConfig       = errors.New("invalid kirari client config")
	ErrIssuerNotAllowed    = errors.New("kirari issuer is not allowed")
	ErrMissingAccessToken  = errors.New("access token is required")
	ErrMissingRefreshToken = errors.New("refresh token is required")
	ErrMissingCode         = errors.New("authorization code is required")
	ErrMissingSubject      = errors.New("oidc subject is required")
	ErrSubjectMismatch     = errors.New("userinfo subject does not match id token subject")
)

type Config struct {
	IssuerURL                  string
	ClientID                   string
	ClientSecret               string
	RedirectURL                string
	Scopes                     []string
	AllowedIssuers             []string
	MetaEndpointURL            string
	ChatCompletionsEndpointURL string
}

type Client struct {
	cfg        Config
	httpClient *http.Client
}

type TokenStore interface {
	LoadTokenSet(ctx context.Context, subject string) (TokenSet, error)
	SaveTokenSet(ctx context.Context, subject string, tokenSet TokenSet) error
	DeleteTokenSet(ctx context.Context, subject string) error
}

type DiscoveryDocument struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	LLMMetaEndpoint       string   `json:"llm_meta_endpoint,omitempty"`
	LLMChatCompletions    string   `json:"llm_chat_completions_endpoint,omitempty"`
	LLMSupportedScopes    []string `json:"llm_supported_scopes,omitempty"`
}

type AuthorizationSession struct {
	State               string
	Nonce               string
	CodeVerifier        string
	CodeChallenge       string
	CodeChallengeMethod string
}

type AuthorizationOptions struct {
	State     string
	Nonce     string
	Prompt    string
	LoginHint string
	Extra     map[string]string
}

type TokenSet struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
}

type Identity struct {
	Subject           string         `json:"sub"`
	Email             string         `json:"email,omitempty"`
	EmailVerified     bool           `json:"email_verified,omitempty"`
	Name              string         `json:"name,omitempty"`
	PreferredUsername string         `json:"preferred_username,omitempty"`
	Claims            map[string]any `json:"claims,omitempty"`
}

type TokenExchangeResult struct {
	TokenSet TokenSet `json:"token_set"`
	Identity Identity `json:"identity"`
}

func NewClient(cfg Config, httpClient *http.Client) (*Client, error) {
	cfg = normalizeConfig(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{cfg: cfg, httpClient: httpClient}, nil
}

func (cfg Config) Validate() error {
	if !isAbsoluteHTTPURL(cfg.IssuerURL) {
		return fmt.Errorf("%w: issuer_url must be an absolute http or https URL", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return fmt.Errorf("%w: client_id is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return fmt.Errorf("%w: client_secret is required", ErrInvalidConfig)
	}
	if !isAbsoluteHTTPURL(cfg.RedirectURL) {
		return fmt.Errorf("%w: redirect_url must be an absolute http or https URL", ErrInvalidConfig)
	}
	for _, endpoint := range []string{cfg.MetaEndpointURL, cfg.ChatCompletionsEndpointURL} {
		if strings.TrimSpace(endpoint) == "" {
			continue
		}
		if !isAbsoluteHTTPURL(endpoint) {
			return fmt.Errorf("%w: explicit upstream endpoints must be absolute http or https URLs", ErrInvalidConfig)
		}
	}
	return nil
}

func (c *Client) Discover(ctx context.Context) (DiscoveryDocument, error) {
	if c == nil {
		return DiscoveryDocument{}, ErrInvalidConfig
	}
	discoveryURL, err := discoveryURL(c.cfg.IssuerURL)
	if err != nil {
		return DiscoveryDocument{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return DiscoveryDocument{}, fmt.Errorf("build discovery request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return DiscoveryDocument{}, fmt.Errorf("perform discovery request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return DiscoveryDocument{}, fmt.Errorf("oidc discovery failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var doc DiscoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return DiscoveryDocument{}, fmt.Errorf("decode discovery document: %w", err)
	}
	if err := validateDiscoveryDocument(doc); err != nil {
		return DiscoveryDocument{}, err
	}
	if err := ensureIssuerAllowed(doc.Issuer, c.cfg.AllowedIssuers); err != nil {
		return DiscoveryDocument{}, err
	}
	return doc, nil
}

func (c *Client) AuthorizationURL(ctx context.Context, options AuthorizationOptions) (string, AuthorizationSession, error) {
	doc, err := c.Discover(ctx)
	if err != nil {
		return "", AuthorizationSession{}, err
	}
	session, err := newAuthorizationSession(options)
	if err != nil {
		return "", AuthorizationSession{}, err
	}
	oauthConfig := c.oauth2Config(doc)
	authOptions := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("nonce", session.Nonce),
		oauth2.SetAuthURLParam("code_challenge", session.CodeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", session.CodeChallengeMethod),
	}
	if prompt := strings.TrimSpace(options.Prompt); prompt != "" {
		authOptions = append(authOptions, oauth2.SetAuthURLParam("prompt", prompt))
	}
	if loginHint := strings.TrimSpace(options.LoginHint); loginHint != "" {
		authOptions = append(authOptions, oauth2.SetAuthURLParam("login_hint", loginHint))
	}
	for key, value := range options.Extra {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		authOptions = append(authOptions, oauth2.SetAuthURLParam(key, value))
	}
	return oauthConfig.AuthCodeURL(session.State, authOptions...), session, nil
}

func (c *Client) ExchangeCode(ctx context.Context, code string, session AuthorizationSession) (TokenExchangeResult, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return TokenExchangeResult{}, ErrMissingCode
	}
	doc, err := c.Discover(ctx)
	if err != nil {
		return TokenExchangeResult{}, err
	}
	token, err := c.oauth2Config(doc).Exchange(c.clientContext(ctx), code, oauth2.SetAuthURLParam("code_verifier", strings.TrimSpace(session.CodeVerifier)))
	if err != nil {
		return TokenExchangeResult{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	result, err := c.buildTokenExchangeResult(ctx, doc, token)
	if err != nil {
		return TokenExchangeResult{}, err
	}
	return result, nil
}

func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (TokenSet, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return TokenSet{}, ErrMissingRefreshToken
	}
	doc, err := c.Discover(ctx)
	if err != nil {
		return TokenSet{}, err
	}
	source := c.oauth2Config(doc).TokenSource(c.clientContext(ctx), &oauth2.Token{
		RefreshToken: refreshToken,
		Expiry:       time.Now().Add(-time.Hour),
	})
	token, err := source.Token()
	if err != nil {
		return TokenSet{}, fmt.Errorf("refresh access token: %w", err)
	}
	return tokenSetFromOAuthToken(token), nil
}

func (c *Client) UserInfo(ctx context.Context, accessToken string) (Identity, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return Identity{}, ErrMissingAccessToken
	}
	doc, err := c.Discover(ctx)
	if err != nil {
		return Identity{}, err
	}
	provider, err := oidc.NewProvider(c.clientContext(ctx), doc.Issuer)
	if err != nil {
		return Identity{}, fmt.Errorf("create oidc provider: %w", err)
	}
	info, err := provider.UserInfo(c.clientContext(ctx), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken}))
	if err != nil {
		return Identity{}, fmt.Errorf("fetch userinfo: %w", err)
	}
	return decodeIdentityClaims(info.Subject, info)
}

func (c *Client) Meta(ctx context.Context, accessToken string) (map[string]any, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, ErrMissingAccessToken
	}
	doc, err := c.Discover(ctx)
	if err != nil {
		return nil, err
	}
	endpoint, err := c.metaEndpoint(doc)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build meta request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform meta request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("meta request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode meta response: %w", err)
	}
	return payload, nil
}

func (c *Client) ChatCompletions(ctx context.Context, accessToken string, body any) (*http.Response, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, ErrMissingAccessToken
	}
	doc, err := c.Discover(ctx)
	if err != nil {
		return nil, err
	}
	endpoint, err := c.chatCompletionsEndpoint(doc)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("build chat completions request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform chat completions request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("chat completions request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func (c *Client) buildTokenExchangeResult(ctx context.Context, doc DiscoveryDocument, token *oauth2.Token) (TokenExchangeResult, error) {
	if token == nil {
		return TokenExchangeResult{}, errors.New("token exchange returned nil token")
	}
	tokenSet := tokenSetFromOAuthToken(token)
	rawIDToken, _ := token.Extra("id_token").(string)
	if strings.TrimSpace(rawIDToken) == "" {
		return TokenExchangeResult{}, errors.New("token exchange response is missing id_token")
	}
	provider, err := oidc.NewProvider(c.clientContext(ctx), doc.Issuer)
	if err != nil {
		return TokenExchangeResult{}, fmt.Errorf("create oidc provider: %w", err)
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: c.cfg.ClientID}).Verify(c.clientContext(ctx), rawIDToken)
	if err != nil {
		return TokenExchangeResult{}, fmt.Errorf("verify id token: %w", err)
	}
	identity, err := decodeIdentityClaims(idToken.Subject, idToken)
	if err != nil {
		return TokenExchangeResult{}, err
	}
	if strings.TrimSpace(token.AccessToken) != "" && strings.TrimSpace(doc.UserInfoEndpoint) != "" {
		info, err := provider.UserInfo(c.clientContext(ctx), oauth2.StaticTokenSource(token))
		if err != nil {
			return TokenExchangeResult{}, fmt.Errorf("fetch userinfo: %w", err)
		}
		merged, err := mergeIdentityClaims(identity, info)
		if err != nil {
			return TokenExchangeResult{}, err
		}
		identity = merged
	}
	tokenSet.IDToken = rawIDToken
	return TokenExchangeResult{TokenSet: tokenSet, Identity: identity}, nil
}

func (c *Client) oauth2Config(doc DiscoveryDocument) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.cfg.ClientID,
		ClientSecret: c.cfg.ClientSecret,
		RedirectURL:  c.cfg.RedirectURL,
		Scopes:       normalizedScopes(c.cfg.Scopes),
		Endpoint: oauth2.Endpoint{
			AuthURL:  doc.AuthorizationEndpoint,
			TokenURL: doc.TokenEndpoint,
		},
	}
}

func (c *Client) metaEndpoint(doc DiscoveryDocument) (string, error) {
	return resolveEndpoint(doc.LLMMetaEndpoint, c.cfg.MetaEndpointURL, doc.Issuer, "/api/llm/meta")
}

func (c *Client) chatCompletionsEndpoint(doc DiscoveryDocument) (string, error) {
	return resolveEndpoint(doc.LLMChatCompletions, c.cfg.ChatCompletionsEndpointURL, doc.Issuer, "/api/llm/chat/completions")
}

func (c *Client) clientContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, c.httpClient)
}

func normalizeConfig(cfg Config) Config {
	cfg.IssuerURL = strings.TrimSpace(cfg.IssuerURL)
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	cfg.RedirectURL = strings.TrimSpace(cfg.RedirectURL)
	cfg.MetaEndpointURL = strings.TrimSpace(cfg.MetaEndpointURL)
	cfg.ChatCompletionsEndpointURL = strings.TrimSpace(cfg.ChatCompletionsEndpointURL)
	cfg.Scopes = normalizedScopes(cfg.Scopes)
	cfg.AllowedIssuers = normalizedStringList(cfg.AllowedIssuers)
	return cfg
}

func normalizedScopes(values []string) []string {
	if len(values) == 0 {
		return []string{"openid", "profile", "email", "offline_access", "llm:read", "llm:stream"}
	}
	return normalizedStringList(values)
}

func normalizedStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validateDiscoveryDocument(doc DiscoveryDocument) error {
	if !isAbsoluteHTTPURL(doc.Issuer) || !isAbsoluteHTTPURL(doc.AuthorizationEndpoint) || !isAbsoluteHTTPURL(doc.TokenEndpoint) || !isAbsoluteHTTPURL(doc.JWKSURI) {
		return fmt.Errorf("%w: discovery document is missing required absolute endpoints", ErrInvalidConfig)
	}
	if strings.TrimSpace(doc.UserInfoEndpoint) != "" && !isAbsoluteHTTPURL(doc.UserInfoEndpoint) {
		return fmt.Errorf("%w: userinfo_endpoint must be an absolute http or https URL", ErrInvalidConfig)
	}
	if strings.TrimSpace(doc.LLMMetaEndpoint) != "" && !isAbsoluteHTTPURL(doc.LLMMetaEndpoint) {
		return fmt.Errorf("%w: llm_meta_endpoint must be an absolute http or https URL", ErrInvalidConfig)
	}
	if strings.TrimSpace(doc.LLMChatCompletions) != "" && !isAbsoluteHTTPURL(doc.LLMChatCompletions) {
		return fmt.Errorf("%w: llm_chat_completions_endpoint must be an absolute http or https URL", ErrInvalidConfig)
	}
	return nil
}

func ensureIssuerAllowed(issuer string, allowed []string) error {
	issuer = normalizeURLString(issuer)
	if len(allowed) == 0 {
		return nil
	}
	for _, item := range allowed {
		if normalizeURLString(item) == issuer {
			return nil
		}
	}
	return ErrIssuerNotAllowed
}

func newAuthorizationSession(options AuthorizationOptions) (AuthorizationSession, error) {
	state := strings.TrimSpace(options.State)
	nonce := strings.TrimSpace(options.Nonce)
	if state == "" {
		value, err := randomString(32)
		if err != nil {
			return AuthorizationSession{}, err
		}
		state = value
	}
	if nonce == "" {
		value, err := randomString(32)
		if err != nil {
			return AuthorizationSession{}, err
		}
		nonce = value
	}
	codeVerifier, err := randomString(64)
	if err != nil {
		return AuthorizationSession{}, err
	}
	sum := sha256.Sum256([]byte(codeVerifier))
	return AuthorizationSession{
		State:               state,
		Nonce:               nonce,
		CodeVerifier:        codeVerifier,
		CodeChallenge:       base64.RawURLEncoding.EncodeToString(sum[:]),
		CodeChallengeMethod: "S256",
	}, nil
}

func tokenSetFromOAuthToken(token *oauth2.Token) TokenSet {
	if token == nil {
		return TokenSet{}
	}
	scope, _ := token.Extra("scope").(string)
	idToken, _ := token.Extra("id_token").(string)
	return TokenSet{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry.UTC(),
		Scope:        strings.TrimSpace(scope),
		IDToken:      strings.TrimSpace(idToken),
	}
}

func decodeIdentityClaims(subject string, source interface{ Claims(any) error }) (Identity, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return Identity{}, ErrMissingSubject
	}
	var claims map[string]any
	if err := source.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("decode oidc claims: %w", err)
	}
	identity := Identity{
		Subject:           subject,
		Email:             stringValue(claims["email"]),
		EmailVerified:     boolValue(claims["email_verified"]),
		Name:              stringValue(claims["name"]),
		PreferredUsername: stringValue(firstNonNil(claims["preferred_username"], claims["preferred_name"])),
		Claims:            claims,
	}
	return identity, nil
}

func mergeIdentityClaims(identity Identity, userInfo *oidc.UserInfo) (Identity, error) {
	if userInfo == nil {
		return identity, nil
	}
	if strings.TrimSpace(userInfo.Subject) == "" {
		return identity, nil
	}
	if strings.TrimSpace(userInfo.Subject) != strings.TrimSpace(identity.Subject) {
		return Identity{}, ErrSubjectMismatch
	}
	infoIdentity, err := decodeIdentityClaims(userInfo.Subject, userInfo)
	if err != nil {
		return Identity{}, err
	}
	if infoIdentity.Email != "" {
		identity.Email = infoIdentity.Email
	}
	if infoIdentity.EmailVerified {
		identity.EmailVerified = true
	}
	if infoIdentity.Name != "" {
		identity.Name = infoIdentity.Name
	}
	if infoIdentity.PreferredUsername != "" {
		identity.PreferredUsername = infoIdentity.PreferredUsername
	}
	identity.Claims = mergeClaimMaps(identity.Claims, infoIdentity.Claims)
	return identity, nil
}

func mergeClaimMaps(left map[string]any, right map[string]any) map[string]any {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	merged := make(map[string]any, len(left)+len(right))
	for key, value := range left {
		merged[key] = value
	}
	for key, value := range right {
		merged[key] = value
	}
	return merged
}

func resolveEndpoint(fromDiscovery string, fromConfig string, issuer string, fallbackPath string) (string, error) {
	for _, candidate := range []string{strings.TrimSpace(fromDiscovery), strings.TrimSpace(fromConfig)} {
		if candidate == "" {
			continue
		}
		if !isAbsoluteHTTPURL(candidate) {
			return "", fmt.Errorf("%w: endpoint %q must be an absolute http or https URL", ErrInvalidConfig, candidate)
		}
		return candidate, nil
	}
	base, err := neturl.Parse(strings.TrimSpace(issuer))
	if err != nil {
		return "", fmt.Errorf("parse issuer url: %w", err)
	}
	fallback, err := base.Parse(fallbackPath)
	if err != nil {
		return "", fmt.Errorf("resolve fallback endpoint: %w", err)
	}
	return fallback.String(), nil
}

func discoveryURL(issuer string) (string, error) {
	parsed, err := neturl.Parse(strings.TrimSpace(issuer))
	if err != nil {
		return "", fmt.Errorf("parse issuer url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: issuer_url must be an absolute http or https URL", ErrInvalidConfig)
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if path == "" {
		parsed.Path = "/.well-known/openid-configuration"
	} else {
		parsed.Path = path + "/.well-known/openid-configuration"
	}
	return parsed.String(), nil
}

func isAbsoluteHTTPURL(raw string) bool {
	parsed, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return parsed.IsAbs() && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func normalizeURLString(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/")
	return raw
}

func randomString(size int) (string, error) {
	if size <= 0 {
		size = 32
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random string: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
