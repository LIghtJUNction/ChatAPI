package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	platformemail "github.com/zyf/chatapi/internal/platform/email"
	"github.com/zyf/chatapi/internal/store"
)

const (
	emailCodePurposeRegister      = "register"
	emailCodePurposePasswordReset = "password_reset"
	defaultEmailCodeTTL           = 10 * time.Minute
	emailCodeResendInterval       = 60 * time.Second
	emailCodeMaxFailedAttempts    = 5
)

var ErrEmailCodeInvalid = errors.New("email verification code is invalid")
var ErrEmailCodeExpired = errors.New("email verification code has expired")
var ErrEmailDeliveryUnavailable = errors.New("email delivery is unavailable")
var ErrEmailCodeRateLimited = errors.New("email verification code rate limited")
var ErrEmailCodeTooManyAttempts = errors.New("email verification code has too many failed attempts")

type EmailCodeService struct {
	store     store.Store
	sender    platformemail.Sender
	smtpCfg   platformemail.SMTPConfig
	masterKey string
	now       func() time.Time
}

func NewEmailCodeService(dataStore store.Store, masterKey string, smtpCfg platformemail.SMTPConfig, sender platformemail.Sender) *EmailCodeService {
	if sender == nil {
		sender = platformemail.NewSMTPSender(smtpCfg)
	}
	return &EmailCodeService{
		store:     dataStore,
		sender:    sender,
		smtpCfg:   smtpCfg,
		masterKey: strings.TrimSpace(masterKey),
		now:       time.Now,
	}
}

func (s *EmailCodeService) SendCode(ctx context.Context, email string, purpose string, subject string, bodyPrefix string) error {
	if s == nil || s.store == nil || s.sender == nil || strings.TrimSpace(s.masterKey) == "" {
		return ErrEmailDeliveryUnavailable
	}
	check := platformemail.CheckSMTPConfig(s.smtpCfg)
	if !check.OK {
		return ErrEmailDeliveryUnavailable
	}
	email = normalizeEmailAddress(email)
	purpose = strings.TrimSpace(purpose)
	if email == "" || purpose == "" {
		return ErrInvalidUserInput
	}
	if existing, err := s.store.GetAuthVerificationCode(ctx, email, purpose); err == nil {
		if !existing.LastSentAt.IsZero() && s.now().UTC().Before(existing.LastSentAt.Add(emailCodeResendInterval)) {
			return ErrEmailCodeRateLimited
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	code, err := generateNumericCode(6)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	expiresAt := now.Add(defaultEmailCodeTTL)
	if _, err := s.store.UpsertAuthVerificationCode(ctx, store.UpsertAuthVerificationCodeInput{
		Email:          email,
		Purpose:        purpose,
		CodeHash:       s.hash(email, purpose, code),
		FailedAttempts: 0,
		ExpiresAt:      expiresAt,
		LastSentAt:     now,
	}); err != nil {
		return err
	}
	message := platformemail.Message{
		To:      []string{email},
		Subject: subject,
		Text:    fmt.Sprintf("%s\n\n验证码：%s\n有效期：10 分钟。\n", strings.TrimSpace(bodyPrefix), code),
	}
	return s.sender.Send(ctx, message)
}

func (s *EmailCodeService) VerifyCode(ctx context.Context, email string, purpose string, code string) error {
	if s == nil || s.store == nil || strings.TrimSpace(s.masterKey) == "" {
		return ErrEmailCodeInvalid
	}
	email = normalizeEmailAddress(email)
	purpose = strings.TrimSpace(purpose)
	code = strings.TrimSpace(code)
	if email == "" || purpose == "" || code == "" {
		return ErrEmailCodeInvalid
	}
	item, err := s.store.GetAuthVerificationCode(ctx, email, purpose)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrEmailCodeInvalid
		}
		return err
	}
	if item.ExpiresAt.Before(s.now().UTC()) {
		_ = s.store.DeleteAuthVerificationCode(ctx, email, purpose)
		return ErrEmailCodeExpired
	}
	if item.FailedAttempts >= emailCodeMaxFailedAttempts {
		_ = s.store.DeleteAuthVerificationCode(ctx, email, purpose)
		return ErrEmailCodeTooManyAttempts
	}
	if !hmac.Equal([]byte(item.CodeHash), []byte(s.hash(email, purpose, code))) {
		_, _ = s.store.UpsertAuthVerificationCode(ctx, store.UpsertAuthVerificationCodeInput{
			Email:          item.Email,
			Purpose:        item.Purpose,
			CodeHash:       item.CodeHash,
			FailedAttempts: item.FailedAttempts + 1,
			ExpiresAt:      item.ExpiresAt,
			LastSentAt:     item.LastSentAt,
		})
		return ErrEmailCodeInvalid
	}
	if err := s.store.DeleteAuthVerificationCode(ctx, email, purpose); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

func (s *EmailCodeService) CleanupExpired(ctx context.Context, now time.Time) (int, error) {
	if s == nil || s.store == nil {
		return 0, nil
	}
	return s.store.DeleteExpiredAuthVerificationCodes(ctx, now.UTC())
}

func (s *EmailCodeService) hash(email string, purpose string, code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(email)) + "|" + strings.TrimSpace(purpose) + "|" + strings.TrimSpace(code) + "|" + s.masterKey))
	return hex.EncodeToString(sum[:])
}

func generateNumericCode(length int) (string, error) {
	if length <= 0 {
		return "", ErrInvalidUserInput
	}
	out := make([]byte, length)
	for i := range out {
		var buf [1]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return "", err
		}
		out[i] = byte('0' + int(buf[0])%10)
	}
	return string(out), nil
}

func normalizeEmailAddress(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func PurposeRegister() string             { return emailCodePurposeRegister }
func PurposePasswordReset() string        { return emailCodePurposePasswordReset }
func EmailCodeTTLSeconds() int            { return int(defaultEmailCodeTTL / time.Second) }
func EmailCodeResendIntervalSeconds() int { return int(emailCodeResendInterval / time.Second) }

func NumericCodeLength() int          { return 6 }
func EmailCodeMaxFailedAttempts() int { return emailCodeMaxFailedAttempts }

func ParseEmailCode(raw string) string {
	return strings.TrimSpace(raw)
}

func codeSubject(purpose string) string {
	switch purpose {
	case emailCodePurposeRegister:
		return "ChatAPI 注册验证码"
	case emailCodePurposePasswordReset:
		return "ChatAPI 重置密码验证码"
	default:
		return "ChatAPI 邮箱验证码"
	}
}

func codeBodyPrefix(purpose string) string {
	switch purpose {
	case emailCodePurposeRegister:
		return "你正在注册 ChatAPI 账号。"
	case emailCodePurposePasswordReset:
		return "你正在重置 ChatAPI 密码。"
	default:
		return "你正在使用 ChatAPI 邮箱验证。"
	}
}

func atoiOrZero(raw string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(raw))
	return value
}
