package verification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/platform/email"
	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

const (
	PurposeRegister      = "register"
	PurposePasswordReset = "password_reset"
)

var (
	ErrInvalidPurpose    = errors.New("invalid verification purpose")
	ErrDeliveryDisabled  = errors.New("verification delivery is disabled")
	ErrCodeRequired      = errors.New("verification code is required")
	ErrCodeNotFound      = errors.New("verification code not found")
	ErrCodeExpired       = errors.New("verification code expired")
	ErrCodeInvalid       = errors.New("verification code invalid")
	ErrCodeRateLimited   = errors.New("verification code rate limited")
	ErrCodeAttemptsLimit = errors.New("verification code attempts exceeded")
)

type Service struct {
	store          store.Store
	sender         email.Sender
	now            func() time.Time
	generateCode   func() (string, error)
	ttl            time.Duration
	resendCooldown time.Duration
	maxAttempts    int
	Logger         *zap.Logger
}

type SendResult struct {
	Email     string    `json:"email"`
	Purpose   string    `json:"purpose"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewService(dataStore store.Store, sender email.Sender) *Service {
	return &Service{
		store:          dataStore,
		sender:         sender,
		now:            func() time.Time { return time.Now().UTC() },
		generateCode:   randomNumericCode,
		ttl:            10 * time.Minute,
		resendCooldown: time.Minute,
		maxAttempts:    5,
	}
}

func (s *Service) SendCode(ctx context.Context, emailAddress string, purpose string) (SendResult, error) {
	emailAddress = normalizeEmail(emailAddress)
	purpose = normalizePurpose(purpose)
	if err := validatePurpose(purpose); err != nil {
		return SendResult{}, err
	}
	if emailAddress == "" {
		return SendResult{}, ErrCodeRequired
	}
	if s.sender == nil {
		return SendResult{}, ErrDeliveryDisabled
	}
	now := s.now()
	if existing, err := s.store.GetAuthVerificationCode(ctx, emailAddress, purpose); err == nil {
		if now.Sub(existing.LastSentAt) < s.resendCooldown {
			return SendResult{}, ErrCodeRateLimited
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return SendResult{}, err
	}

	code, err := s.generateCode()
	if err != nil {
		return SendResult{}, err
	}
	expiresAt := now.Add(s.ttl)
	if _, err := s.store.UpsertAuthVerificationCode(ctx, store.UpsertAuthVerificationCodeInput{
		Email:          emailAddress,
		Purpose:        purpose,
		CodeHash:       hashCode(code),
		FailedAttempts: 0,
		ExpiresAt:      expiresAt,
		LastSentAt:     now,
	}); err != nil {
		return SendResult{}, err
	}
	if err := s.sender.Send(ctx, email.Message{
		To:      []string{emailAddress},
		Subject: subjectForPurpose(purpose),
		Text:    textForPurpose(purpose, code, expiresAt),
	}); err != nil {
		return SendResult{}, err
	}
	logging.BindContext(s.Logger, ctx,
		zap.String("auth.kind", "verification"),
		zap.String("verification.purpose", purpose),
		zap.String("email", emailAddress),
	).Info("verification code sent")
	return SendResult{Email: emailAddress, Purpose: purpose, ExpiresAt: expiresAt}, nil
}

func (s *Service) VerifyCode(ctx context.Context, emailAddress string, purpose string, code string) error {
	emailAddress = normalizeEmail(emailAddress)
	purpose = normalizePurpose(purpose)
	code = strings.TrimSpace(code)
	if err := validatePurpose(purpose); err != nil {
		return err
	}
	if code == "" {
		return ErrCodeRequired
	}
	item, err := s.store.GetAuthVerificationCode(ctx, emailAddress, purpose)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrCodeNotFound
		}
		return err
	}
	now := s.now()
	if !item.ExpiresAt.After(now) {
		_ = s.store.DeleteAuthVerificationCode(ctx, emailAddress, purpose)
		return ErrCodeExpired
	}
	if subtle.ConstantTimeCompare([]byte(hashCode(code)), []byte(item.CodeHash)) != 1 {
		nextAttempts := item.FailedAttempts + 1
		if nextAttempts >= s.maxAttempts {
			_ = s.store.DeleteAuthVerificationCode(ctx, emailAddress, purpose)
			return ErrCodeAttemptsLimit
		}
		_, updateErr := s.store.UpsertAuthVerificationCode(ctx, store.UpsertAuthVerificationCodeInput{
			Email:          emailAddress,
			Purpose:        purpose,
			CodeHash:       item.CodeHash,
			FailedAttempts: nextAttempts,
			ExpiresAt:      item.ExpiresAt,
			LastSentAt:     item.LastSentAt,
		})
		if updateErr != nil {
			return updateErr
		}
		return ErrCodeInvalid
	}
	if err := s.store.DeleteAuthVerificationCode(ctx, emailAddress, purpose); err != nil {
		return err
	}
	return nil
}

func randomNumericCode() (string, error) {
	max := big.NewInt(1000000)
	value, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return fmt.Sprintf("%x", sum[:])
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizePurpose(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validatePurpose(purpose string) error {
	switch purpose {
	case PurposeRegister, PurposePasswordReset:
		return nil
	default:
		return ErrInvalidPurpose
	}
}

func subjectForPurpose(purpose string) string {
	switch purpose {
	case PurposePasswordReset:
		return "ChatAPI password reset code"
	default:
		return "ChatAPI verification code"
	}
}

func textForPurpose(purpose string, code string, expiresAt time.Time) string {
	label := "verification"
	if purpose == PurposePasswordReset {
		label = "password reset"
	}
	return fmt.Sprintf("Your ChatAPI %s code is %s.\n\nIt expires at %s UTC.", label, code, expiresAt.UTC().Format(time.RFC3339))
}
