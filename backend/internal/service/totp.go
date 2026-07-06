package service

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

	"github.com/zyf/chatapi/internal/platform/secretbox"
	"github.com/zyf/chatapi/internal/store"
)

const totpUserConfigKey = "security.totp"

var ErrInvalidTOTPInput = errors.New("invalid totp input")
var ErrTOTPNotConfigured = errors.New("totp is not configured")
var ErrTOTPCodeInvalid = errors.New("totp code is invalid")

type TOTPSetup struct {
	Secret   string `json:"secret"`
	URI      string `json:"uri"`
	QRBase64 string `json:"qr_base64"`
}

type TOTPService struct {
	store     store.Store
	masterKey string
	issuer    string
	now       func() time.Time
}

type totpConfigRecord struct {
	SecretCiphertext string
	Enabled          bool
}

func NewTOTPService(dataStore store.Store, masterKey string, issuer string) *TOTPService {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		issuer = "ChatAPI"
	}
	return &TOTPService{
		store:     dataStore,
		masterKey: strings.TrimSpace(masterKey),
		issuer:    issuer,
		now:       time.Now,
	}
}

func (s *TOTPService) Setup(ctx context.Context, userID string, accountName string) (TOTPSetup, error) {
	if s == nil || s.store == nil || strings.TrimSpace(s.masterKey) == "" {
		return TOTPSetup{}, ErrInvalidTOTPInput
	}
	userID = strings.TrimSpace(userID)
	accountName = strings.TrimSpace(accountName)
	if userID == "" || accountName == "" {
		return TOTPSetup{}, ErrInvalidTOTPInput
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		return TOTPSetup{}, err
	}
	ciphertext, err := secretbox.Seal(secret, s.masterKey)
	if err != nil {
		return TOTPSetup{}, err
	}
	uri := buildTOTPURI(s.issuer, accountName, secret)
	if _, err := s.store.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: userID,
		Key:    totpUserConfigKey,
		Value: map[string]any{
			"secret_ciphertext": ciphertext,
			"enabled":           false,
			"updated_at":        s.now().UTC().Format(time.RFC3339),
		},
	}); err != nil {
		return TOTPSetup{}, err
	}
	qrPNG, err := qrcode.Encode(uri, qrcode.Medium, 200)
	if err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{
		Secret:   secret,
		URI:      uri,
		QRBase64: base64.StdEncoding.EncodeToString(qrPNG),
	}, nil
}

func (s *TOTPService) Confirm(ctx context.Context, userID string, secret string, code string) error {
	record, err := s.loadConfig(ctx, userID)
	if err != nil {
		return err
	}
	storedSecret, err := secretbox.Open(record.SecretCiphertext, s.masterKey)
	if err != nil {
		return err
	}
	if storedSecret != strings.TrimSpace(secret) {
		return ErrTOTPCodeInvalid
	}
	if !totp.Validate(strings.TrimSpace(code), storedSecret) {
		return ErrTOTPCodeInvalid
	}
	_, err = s.store.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: strings.TrimSpace(userID),
		Key:    totpUserConfigKey,
		Value: map[string]any{
			"secret_ciphertext": record.SecretCiphertext,
			"enabled":           true,
			"updated_at":        s.now().UTC().Format(time.RFC3339),
		},
	})
	return err
}

func (s *TOTPService) Reset(ctx context.Context, userID string) error {
	if s == nil || s.store == nil {
		return ErrInvalidTOTPInput
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ErrInvalidTOTPInput
	}
	return s.store.DeleteUserConfig(ctx, userID, totpUserConfigKey)
}

func (s *TOTPService) IsEnabled(ctx context.Context, userID string) bool {
	record, err := s.loadConfig(ctx, userID)
	return err == nil && record.Enabled
}

func (s *TOTPService) ValidateLoginCode(ctx context.Context, userID string, code string) error {
	record, err := s.loadConfig(ctx, userID)
	if err != nil {
		return err
	}
	if !record.Enabled {
		return ErrTOTPNotConfigured
	}
	secret, err := secretbox.Open(record.SecretCiphertext, s.masterKey)
	if err != nil {
		return err
	}
	if !totp.Validate(strings.TrimSpace(code), secret) {
		return ErrTOTPCodeInvalid
	}
	return nil
}

func (s *TOTPService) loadConfig(ctx context.Context, userID string) (totpConfigRecord, error) {
	if s == nil || s.store == nil || strings.TrimSpace(s.masterKey) == "" {
		return totpConfigRecord{}, ErrInvalidTOTPInput
	}
	item, err := s.store.GetUserConfig(ctx, strings.TrimSpace(userID), totpUserConfigKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return totpConfigRecord{}, ErrTOTPNotConfigured
		}
		return totpConfigRecord{}, err
	}
	return totpConfigRecord{
		SecretCiphertext: stringValue(item.Value["secret_ciphertext"], ""),
		Enabled:          boolValue(item.Value["enabled"], false),
	}, nil
}

func generateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate totp secret: %w", err)
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(raw), "="), nil
}

func buildTOTPURI(issuer string, accountName string, secret string) string {
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
	typed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return typed
}
