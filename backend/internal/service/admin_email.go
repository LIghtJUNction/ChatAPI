package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	platformemail "github.com/zyf/chatapi/internal/platform/email"
)

var ErrInvalidAdminEmailInput = errors.New("invalid admin email input")
var ErrEmailConfigInvalid = errors.New("email config invalid")

type AdminEmailService struct {
	sender platformemail.Sender
	cfg    platformemail.SMTPConfig
}

func NewAdminEmailService(cfg platformemail.SMTPConfig, sender platformemail.Sender) *AdminEmailService {
	if sender == nil {
		sender = platformemail.NewSMTPSender(cfg)
	}
	return &AdminEmailService{
		sender: sender,
		cfg:    cfg,
	}
}

func (s *AdminEmailService) SendTestEmail(ctx context.Context, to string) error {
	if s == nil || s.sender == nil {
		return ErrInvalidAdminEmailInput
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return ErrInvalidAdminEmailInput
	}
	check := platformemail.CheckSMTPConfig(s.cfg)
	if !check.OK {
		return fmt.Errorf("%w: %s", ErrEmailConfigInvalid, strings.Join(check.Errors, "; "))
	}
	return s.sender.Send(ctx, platformemail.Message{
		To:      []string{to},
		Subject: "ChatAPI test email",
		Text:    "ChatAPI SMTP test email.",
	})
}
