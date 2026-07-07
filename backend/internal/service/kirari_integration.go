package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/config"
	platformkirari "github.com/zyf/chatapi/internal/platform/kirari"
	"github.com/zyf/chatapi/internal/platform/secretbox"
	"github.com/zyf/chatapi/internal/store"
)

const kirariUserConfigKey = "security.kirari"

var (
	ErrKirariDisabled         = errors.New("kirari integration is disabled")
	ErrKirariNotConnected     = errors.New("kirari integration is not connected")
	ErrKirariInvalidState     = errors.New("kirari integration state is invalid")
	ErrKirariMissingCode      = errors.New("kirari authorization code is required")
	ErrKirariMissingUser      = errors.New("kirari integration user is required")
	ErrKirariMissingMasterKey = errors.New("kirari integration master key is required")
)

type KirariIntegrationService struct {
	store      store.Store
	cfg        config.Config
	httpClient *http.Client
	now        func() time.Time
}

func (s *KirariIntegrationService) ProviderName() string {
	return providerKirari
}

func (s *KirariIntegrationService) ProviderDescriptor() UpstreamProviderDescriptor {
	return UpstreamProviderDescriptor{
		Name:              providerKirari,
		DisplayName:       "KirariNetwork",
		Protocols:         []string{"chat_completions"},
		SupportsStreaming: true,
		RequiresUserLink:  true,
		Capabilities: map[string]any{
			"backend_delegated":            true,
			"browser_direct_recommended":   false,
			"supports_structured_output":   true,
			"supports_native_json_schema":  true,
			"supports_tool_schemas":        true,
			"supports_cancel":              false,
			"supports_model_meta":          true,
			"supports_stream_assist":       true,
			"requires_provider_connection": true,
		},
		Notes: []string{
			"Requires the current user to complete delegated OIDC authorization before backend-side assist can call Kirari.",
			"Uses the provider registry and shared Tool Call assist chat-completions adapter.",
		},
		ErrorCodes: []UpstreamErrorCode{
			{Code: "provider_not_connected", Description: "The current user has not connected the delegated Kirari provider yet."},
			{Code: "provider_request_failed", Description: "The delegated provider request failed or returned an unreadable response."},
		},
		RequestHints: map[string]any{
			"default_protocol":       "chat_completions",
			"supports_stream_toggle": true,
			"supports_response_format": map[string]any{
				"type":         "json_schema",
				"required":     true,
				"schema_field": "response_format.json_schema",
			},
		},
		ResponseHints: map[string]any{
			"stream_event_format": "openai_chat_completions_sse",
			"final_text_paths": []string{
				"choices[0].message.content",
			},
			"stream_delta_paths": []string{
				"choices[0].delta.content",
				"choices[0].delta.content[].text",
			},
		},
	}
}

type KirariStatus struct {
	Enabled                bool           `json:"enabled"`
	Connected              bool           `json:"connected"`
	ProviderName           string         `json:"provider_name"`
	IssuerURL              string         `json:"issuer_url,omitempty"`
	ConfiguredScopes       []string       `json:"configured_scopes,omitempty"`
	GrantedScopes          []string       `json:"granted_scopes,omitempty"`
	Subject                string         `json:"subject,omitempty"`
	ExpiresAt              *time.Time     `json:"expires_at,omitempty"`
	HasRefreshToken        bool           `json:"has_refresh_token"`
	ConnectURL             string         `json:"connect_url,omitempty"`
	CallbackURL            string         `json:"callback_url,omitempty"`
	DisconnectURL          string         `json:"disconnect_url,omitempty"`
	MetaURL                string         `json:"meta_url,omitempty"`
	CachedModelMeta        map[string]any `json:"cached_model_meta,omitempty"`
	CachedModelMetaExpires *time.Time     `json:"cached_model_meta_expires_at,omitempty"`
}

type KirariAuthorizationSession struct {
	State        string `json:"state"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
	UserID       string `json:"user_id"`
}

type kirariStoredConnection struct {
	IssuerURL               string         `json:"issuer_url,omitempty"`
	Subject                 string         `json:"kirari_subject,omitempty"`
	AccessTokenCiphertext   string         `json:"access_token_ciphertext,omitempty"`
	RefreshTokenCiphertext  string         `json:"refresh_token_ciphertext,omitempty"`
	ExpiresAt               *time.Time     `json:"expires_at,omitempty"`
	GrantedScopes           []string       `json:"granted_scopes,omitempty"`
	ModelMetaCache          map[string]any `json:"model_meta_cache_json,omitempty"`
	ModelMetaCacheExpiresAt *time.Time     `json:"model_meta_cache_expires_at,omitempty"`
}

func NewKirariIntegrationService(dataStore store.Store, cfg config.Config, httpClient *http.Client) *KirariIntegrationService {
	return &KirariIntegrationService{
		store:      dataStore,
		cfg:        cfg,
		httpClient: httpClient,
		now:        time.Now,
	}
}

func (s *KirariIntegrationService) Status(ctx context.Context, userID string) (KirariStatus, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return KirariStatus{}, ErrKirariMissingUser
	}
	status := KirariStatus{
		Enabled:          s.cfg.KirariEnabled,
		ProviderName:     "KirariNetwork",
		IssuerURL:        strings.TrimSpace(s.cfg.KirariIssuerURL),
		ConfiguredScopes: append([]string(nil), s.cfg.KirariScopes...),
		ConnectURL:       "/api/user/integrations/kirari/connect",
		CallbackURL:      "/api/integrations/kirari/callback",
		DisconnectURL:    "/api/user/integrations/kirari",
		MetaURL:          "/api/user/integrations/kirari/meta",
	}
	if !s.cfg.KirariEnabled {
		return status, nil
	}
	connection, err := s.loadConnection(ctx, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return status, nil
		}
		return KirariStatus{}, err
	}
	status.Connected = true
	status.Subject = connection.Subject
	status.GrantedScopes = append([]string(nil), connection.GrantedScopes...)
	status.ExpiresAt = connection.ExpiresAt
	status.HasRefreshToken = strings.TrimSpace(connection.RefreshTokenCiphertext) != ""
	status.CachedModelMeta = cloneAnyMap(connection.ModelMetaCache)
	status.CachedModelMetaExpires = connection.ModelMetaCacheExpiresAt
	return status, nil
}

func (s *KirariIntegrationService) StartConnect(ctx context.Context, userID string) (string, KirariAuthorizationSession, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", KirariAuthorizationSession{}, ErrKirariMissingUser
	}
	if !s.cfg.KirariEnabled {
		return "", KirariAuthorizationSession{}, ErrKirariDisabled
	}
	client, err := s.newClient()
	if err != nil {
		return "", KirariAuthorizationSession{}, err
	}
	authURL, session, err := client.AuthorizationURL(ctx, platformkirari.AuthorizationOptions{})
	if err != nil {
		return "", KirariAuthorizationSession{}, err
	}
	return authURL, KirariAuthorizationSession{
		State:        session.State,
		Nonce:        session.Nonce,
		CodeVerifier: session.CodeVerifier,
		UserID:       userID,
	}, nil
}

func (s *KirariIntegrationService) CompleteConnect(ctx context.Context, userID string, code string, session KirariAuthorizationSession) (KirariStatus, error) {
	userID = strings.TrimSpace(userID)
	code = strings.TrimSpace(code)
	if userID == "" {
		return KirariStatus{}, ErrKirariMissingUser
	}
	if code == "" {
		return KirariStatus{}, ErrKirariMissingCode
	}
	if !s.cfg.KirariEnabled {
		return KirariStatus{}, ErrKirariDisabled
	}
	if strings.TrimSpace(session.State) == "" || strings.TrimSpace(session.Nonce) == "" || strings.TrimSpace(session.CodeVerifier) == "" || strings.TrimSpace(session.UserID) != userID {
		return KirariStatus{}, ErrKirariInvalidState
	}
	if strings.TrimSpace(s.cfg.MasterKey) == "" {
		return KirariStatus{}, ErrKirariMissingMasterKey
	}
	client, err := s.newClient()
	if err != nil {
		return KirariStatus{}, err
	}
	result, err := client.ExchangeCode(ctx, code, platformkirari.AuthorizationSession{
		State:               session.State,
		Nonce:               session.Nonce,
		CodeVerifier:        session.CodeVerifier,
		CodeChallenge:       "",
		CodeChallengeMethod: "S256",
	})
	if err != nil {
		return KirariStatus{}, err
	}
	connection, err := s.connectionFromExchangeResult(result)
	if err != nil {
		return KirariStatus{}, err
	}
	if _, err := s.store.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: userID,
		Key:    kirariUserConfigKey,
		Value:  connection.toMap(),
	}); err != nil {
		return KirariStatus{}, err
	}
	return s.Status(ctx, userID)
}

func (s *KirariIntegrationService) Disconnect(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ErrKirariMissingUser
	}
	if !s.cfg.KirariEnabled {
		return ErrKirariDisabled
	}
	if err := s.store.DeleteUserConfig(ctx, userID, kirariUserConfigKey); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

func (s *KirariIntegrationService) Meta(ctx context.Context, userID string, forceRefresh bool) (map[string]any, bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, false, ErrKirariMissingUser
	}
	if !s.cfg.KirariEnabled {
		return nil, false, ErrKirariDisabled
	}
	connection, err := s.loadConnection(ctx, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, ErrKirariNotConnected
		}
		return nil, false, err
	}
	now := s.now().UTC()
	if !forceRefresh && len(connection.ModelMetaCache) > 0 && connection.ModelMetaCacheExpiresAt != nil && connection.ModelMetaCacheExpiresAt.After(now) {
		return cloneAnyMap(connection.ModelMetaCache), true, nil
	}
	client, err := s.newClient()
	if err != nil {
		return nil, false, err
	}
	accessToken, _, connection, err := s.activeAccessToken(ctx, client, userID, connection)
	if err != nil {
		return nil, false, err
	}
	meta, err := client.Meta(ctx, accessToken)
	if err != nil {
		return nil, false, err
	}
	cacheExpiry := now.Add(5 * time.Minute)
	connection.ModelMetaCache = cloneAnyMap(meta)
	connection.ModelMetaCacheExpiresAt = &cacheExpiry
	if _, err := s.store.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: userID,
		Key:    kirariUserConfigKey,
		Value:  connection.toMap(),
	}); err != nil {
		return nil, false, err
	}
	return cloneAnyMap(meta), false, nil
}

func (s *KirariIntegrationService) ChatCompletions(ctx context.Context, userID string, body any) (map[string]any, error) {
	resp, err := s.ChatCompletionsRaw(ctx, userID, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode kirari chat completions response: %w", err)
	}
	return payload, nil
}

func (s *KirariIntegrationService) ChatCompletionsRaw(ctx context.Context, userID string, body any) (*http.Response, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, ErrKirariMissingUser
	}
	if !s.cfg.KirariEnabled {
		return nil, ErrKirariDisabled
	}
	connection, err := s.loadConnection(ctx, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrKirariNotConnected
		}
		return nil, err
	}
	client, err := s.newClient()
	if err != nil {
		return nil, err
	}
	accessToken, _, _, err := s.activeAccessToken(ctx, client, userID, connection)
	if err != nil {
		return nil, err
	}
	return client.ChatCompletions(ctx, accessToken, body)
}

func (s *KirariIntegrationService) newClient() (*platformkirari.Client, error) {
	return platformkirari.NewClient(platformkirari.Config{
		IssuerURL:                  s.cfg.KirariIssuerURL,
		ClientID:                   s.cfg.KirariClientID,
		ClientSecret:               s.cfg.KirariClientSecret,
		RedirectURL:                s.cfg.KirariRedirectURL,
		Scopes:                     s.cfg.KirariScopes,
		AllowedIssuers:             s.cfg.KirariAllowedIssuers,
		MetaEndpointURL:            s.cfg.KirariMetaEndpointURL,
		ChatCompletionsEndpointURL: s.cfg.KirariChatCompletionsEndpointURL,
	}, s.httpClient)
}

func (s *KirariIntegrationService) loadConnection(ctx context.Context, userID string) (kirariStoredConnection, error) {
	item, err := s.store.GetUserConfig(ctx, userID, kirariUserConfigKey)
	if err != nil {
		return kirariStoredConnection{}, err
	}
	return decodeKirariStoredConnection(item.Value)
}

func (s *KirariIntegrationService) connectionFromExchangeResult(result platformkirari.TokenExchangeResult) (kirariStoredConnection, error) {
	accessCiphertext, err := secretbox.Seal(result.TokenSet.AccessToken, s.cfg.MasterKey)
	if err != nil {
		return kirariStoredConnection{}, err
	}
	refreshCiphertext := ""
	if strings.TrimSpace(result.TokenSet.RefreshToken) != "" {
		refreshCiphertext, err = secretbox.Seal(result.TokenSet.RefreshToken, s.cfg.MasterKey)
		if err != nil {
			return kirariStoredConnection{}, err
		}
	}
	var expiresAt *time.Time
	if !result.TokenSet.Expiry.IsZero() {
		expiry := result.TokenSet.Expiry.UTC()
		expiresAt = &expiry
	}
	return kirariStoredConnection{
		IssuerURL:              strings.TrimSpace(s.cfg.KirariIssuerURL),
		Subject:                strings.TrimSpace(result.Identity.Subject),
		AccessTokenCiphertext:  accessCiphertext,
		RefreshTokenCiphertext: refreshCiphertext,
		ExpiresAt:              expiresAt,
		GrantedScopes:          splitScopeString(result.TokenSet.Scope, s.cfg.KirariScopes),
	}, nil
}

func (s *KirariIntegrationService) updateConnectionTokens(ctx context.Context, userID string, connection kirariStoredConnection, tokenSet platformkirari.TokenSet) (kirariStoredConnection, error) {
	accessCiphertext, err := secretbox.Seal(tokenSet.AccessToken, s.cfg.MasterKey)
	if err != nil {
		return kirariStoredConnection{}, err
	}
	refreshCiphertext := connection.RefreshTokenCiphertext
	if strings.TrimSpace(tokenSet.RefreshToken) != "" {
		refreshCiphertext, err = secretbox.Seal(tokenSet.RefreshToken, s.cfg.MasterKey)
		if err != nil {
			return kirariStoredConnection{}, err
		}
	}
	connection.AccessTokenCiphertext = accessCiphertext
	connection.RefreshTokenCiphertext = refreshCiphertext
	if !tokenSet.Expiry.IsZero() {
		expiry := tokenSet.Expiry.UTC()
		connection.ExpiresAt = &expiry
	}
	if scopes := splitScopeString(tokenSet.Scope, nil); len(scopes) > 0 {
		connection.GrantedScopes = scopes
	}
	if _, err := s.store.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: userID,
		Key:    kirariUserConfigKey,
		Value:  connection.toMap(),
	}); err != nil {
		return kirariStoredConnection{}, err
	}
	return connection, nil
}

func (s *KirariIntegrationService) activeAccessToken(ctx context.Context, client *platformkirari.Client, userID string, connection kirariStoredConnection) (string, bool, kirariStoredConnection, error) {
	accessToken, err := secretbox.Open(connection.AccessTokenCiphertext, s.cfg.MasterKey)
	if err != nil {
		return "", false, kirariStoredConnection{}, fmt.Errorf("open kirari access token: %w", err)
	}
	now := s.now().UTC()
	if connection.ExpiresAt == nil || connection.ExpiresAt.IsZero() || connection.ExpiresAt.After(now.Add(30*time.Second)) {
		return accessToken, false, connection, nil
	}
	refreshToken := ""
	if strings.TrimSpace(connection.RefreshTokenCiphertext) != "" {
		refreshToken, err = secretbox.Open(connection.RefreshTokenCiphertext, s.cfg.MasterKey)
		if err != nil {
			return "", false, kirariStoredConnection{}, fmt.Errorf("open kirari refresh token: %w", err)
		}
	}
	if strings.TrimSpace(refreshToken) == "" {
		return "", false, kirariStoredConnection{}, ErrKirariNotConnected
	}
	refreshed, err := client.RefreshToken(ctx, refreshToken)
	if err != nil {
		return "", false, kirariStoredConnection{}, err
	}
	connection, err = s.updateConnectionTokens(ctx, userID, connection, refreshed)
	if err != nil {
		return "", false, kirariStoredConnection{}, err
	}
	accessToken, err = secretbox.Open(connection.AccessTokenCiphertext, s.cfg.MasterKey)
	if err != nil {
		return "", false, kirariStoredConnection{}, fmt.Errorf("open refreshed kirari access token: %w", err)
	}
	return accessToken, true, connection, nil
}

func decodeKirariStoredConnection(value map[string]any) (kirariStoredConnection, error) {
	record := ensureObject(value)
	result := kirariStoredConnection{
		IssuerURL:              strings.TrimSpace(stringValueAny(record["issuer_url"])),
		Subject:                strings.TrimSpace(stringValueAny(record["kirari_subject"])),
		AccessTokenCiphertext:  strings.TrimSpace(stringValueAny(record["access_token_ciphertext"])),
		RefreshTokenCiphertext: strings.TrimSpace(stringValueAny(record["refresh_token_ciphertext"])),
		GrantedScopes:          stringSliceAny(record["granted_scopes"]),
		ModelMetaCache:         ensureObject(anyMap(record["model_meta_cache_json"])),
	}
	if expiresAt, ok := timeValueAny(record["expires_at"]); ok {
		result.ExpiresAt = &expiresAt
	}
	if expiresAt, ok := timeValueAny(record["model_meta_cache_expires_at"]); ok {
		result.ModelMetaCacheExpiresAt = &expiresAt
	}
	if result.Subject == "" || result.AccessTokenCiphertext == "" {
		return kirariStoredConnection{}, ErrKirariNotConnected
	}
	return result, nil
}

func (c kirariStoredConnection) toMap() map[string]any {
	value := map[string]any{
		"issuer_url":               strings.TrimSpace(c.IssuerURL),
		"kirari_subject":           strings.TrimSpace(c.Subject),
		"access_token_ciphertext":  strings.TrimSpace(c.AccessTokenCiphertext),
		"refresh_token_ciphertext": strings.TrimSpace(c.RefreshTokenCiphertext),
		"granted_scopes":           append([]string(nil), c.GrantedScopes...),
	}
	if c.ExpiresAt != nil && !c.ExpiresAt.IsZero() {
		value["expires_at"] = c.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if c.ModelMetaCache != nil {
		value["model_meta_cache_json"] = cloneAnyMap(c.ModelMetaCache)
	}
	if c.ModelMetaCacheExpiresAt != nil && !c.ModelMetaCacheExpiresAt.IsZero() {
		value["model_meta_cache_expires_at"] = c.ModelMetaCacheExpiresAt.UTC().Format(time.RFC3339)
	}
	return value
}

func splitScopeString(raw string, fallback []string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return append([]string(nil), fallback...)
	}
	return normalizedStringSlice(strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}))
}

func normalizedStringSlice(values []string) []string {
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

func stringValueAny(value any) string {
	text, _ := value.(string)
	return text
}

func anyMap(value any) map[string]any {
	record, _ := value.(map[string]any)
	return record
}

func ensureObject(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func stringSliceAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return normalizedStringSlice(typed)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return normalizedStringSlice(result)
	default:
		return nil
	}
}

func timeValueAny(value any) (time.Time, bool) {
	text, _ := value.(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func cloneAnyMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}
