package totp

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"

	"github.com/zyf2007/ChatAPI/internal/platform/secretbox"
	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

const userConfigKey = "security.totp"

var (
	ErrInvalidInput  = errors.New("invalid totp input")
	ErrNotConfigured = errors.New("totp is not configured")
	ErrCodeInvalid   = errors.New("totp code is invalid")
)

type Setup struct {
	Secret   string `json:"secret"`
	URI      string `json:"uri"`
	QRBase64 string `json:"qr_base64"`
}

type Service struct {
	store     auth.SettingsStore
	masterKey string
	issuer    string
	now       func() time.Time
}

type configRecord struct {
	SecretCiphertext string
	Enabled          bool
}

func NewService(dataStore auth.SettingsStore, masterKey string, issuer string) *Service {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		issuer = "ChatAPI"
	}
	return &Service{
		store:     dataStore,
		masterKey: strings.TrimSpace(masterKey),
		issuer:    issuer,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Setup(ctx context.Context, userID string, accountName string) (Setup, error) {
	if s == nil || s.store == nil || s.masterKey == "" {
		return Setup{}, ErrInvalidInput
	}
	userID = strings.TrimSpace(userID)
	accountName = strings.TrimSpace(accountName)
	if userID == "" || accountName == "" {
		return Setup{}, ErrInvalidInput
	}
	secret, err := generateSecret()
	if err != nil {
		return Setup{}, err
	}
	ciphertext, err := secretbox.Seal(secret, s.masterKey)
	if err != nil {
		return Setup{}, err
	}
	uri := buildURI(s.issuer, accountName, secret)
	if _, err := s.store.SetUserConfig(ctx, common.SetUserConfigInput{
		UserID: userID,
		Key:    userConfigKey,
		Value: map[string]any{
			"secret_ciphertext": ciphertext,
			"enabled":           false,
			"updated_at":        s.now().Format(time.RFC3339),
		},
	}); err != nil {
		return Setup{}, err
	}
	qrPNG, err := qrcode.Encode(uri, qrcode.Medium, 200)
	if err != nil {
		return Setup{}, err
	}
	return Setup{
		Secret:   secret,
		URI:      uri,
		QRBase64: base64.StdEncoding.EncodeToString(qrPNG),
	}, nil
}

func (s *Service) Confirm(ctx context.Context, userID string, secret string, code string) error {
	record, err := s.loadConfig(ctx, userID)
	if err != nil {
		return err
	}
	storedSecret, err := secretbox.Open(record.SecretCiphertext, s.masterKey)
	if err != nil {
		return err
	}
	if storedSecret != strings.TrimSpace(secret) {
		return ErrCodeInvalid
	}
	if !totp.Validate(strings.TrimSpace(code), storedSecret) {
		return ErrCodeInvalid
	}
	_, err = s.store.SetUserConfig(ctx, common.SetUserConfigInput{
		UserID: strings.TrimSpace(userID),
		Key:    userConfigKey,
		Value: map[string]any{
			"secret_ciphertext": record.SecretCiphertext,
			"enabled":           true,
			"updated_at":        s.now().Format(time.RFC3339),
		},
	})
	return err
}

func (s *Service) Reset(ctx context.Context, userID string) error {
	if s == nil || s.store == nil {
		return ErrInvalidInput
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ErrInvalidInput
	}
	return s.store.DeleteUserConfig(ctx, userID, userConfigKey)
}

func (s *Service) IsEnabled(ctx context.Context, userID string) bool {
	record, err := s.loadConfig(ctx, userID)
	return err == nil && record.Enabled
}

func (s *Service) ValidateLoginCode(ctx context.Context, userID string, code string) error {
	record, err := s.loadConfig(ctx, userID)
	if err != nil {
		return err
	}
	if !record.Enabled {
		return ErrNotConfigured
	}
	secret, err := secretbox.Open(record.SecretCiphertext, s.masterKey)
	if err != nil {
		return err
	}
	if !totp.Validate(strings.TrimSpace(code), secret) {
		return ErrCodeInvalid
	}
	return nil
}

func (s *Service) loadConfig(ctx context.Context, userID string) (configRecord, error) {
	if s == nil || s.store == nil || s.masterKey == "" {
		return configRecord{}, ErrInvalidInput
	}
	item, err := s.store.GetUserConfig(ctx, strings.TrimSpace(userID), userConfigKey)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			return configRecord{}, ErrNotConfigured
		}
		return configRecord{}, err
	}
	return configRecord{
		SecretCiphertext: stringValue(item.Value["secret_ciphertext"], ""),
		Enabled:          boolValue(item.Value["enabled"], false),
	}, nil
}

func generateSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate totp secret: %w", err)
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(raw), "="), nil
}

func buildURI(issuer string, accountName string, secret string) string {
	key, _ := otp.NewKeyFromURL(fmt.Sprintf(
		"otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		url.PathEscape(issuer),
		url.PathEscape(accountName),
		url.QueryEscape(secret),
		url.QueryEscape(issuer),
	))
	if key != nil {
		return key.URL()
	}
	return fmt.Sprintf(
		"otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		url.PathEscape(issuer),
		url.PathEscape(accountName),
		url.QueryEscape(secret),
		url.QueryEscape(issuer),
	)
}

func boolValue(value any, fallback bool) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	return fallback
}

func stringValue(value any, fallback string) string {
	if typed, ok := value.(string); ok {
		typed = strings.TrimSpace(typed)
		if typed != "" {
			return typed
		}
	}
	return fallback
}
