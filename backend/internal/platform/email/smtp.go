package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/config"
)

type Message struct {
	From    string
	To      []string
	Subject string
	Text    string
}

type Sender interface {
	Send(context.Context, Message) error
}

type SMTPConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	From     string
	Security string
	Timeout  time.Duration
}

type SMTPCheckReport struct {
	OK       bool     `json:"ok"`
	Enabled  bool     `json:"enabled"`
	Host     string   `json:"host,omitempty"`
	Port     int      `json:"port,omitempty"`
	From     string   `json:"from,omitempty"`
	Security string   `json:"security,omitempty"`
	Timeout  string   `json:"timeout,omitempty"`
	Auth     string   `json:"auth,omitempty"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type SMTPSender struct {
	cfg SMTPConfig
}

func SMTPConfigFromConfig(cfg config.Config) SMTPConfig {
	return SMTPConfig{
		Enabled:  cfg.SMTPEnabled,
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.SMTPFrom,
		Security: cfg.SMTPSecurity,
		Timeout:  cfg.SMTPTimeout,
	}
}

func NewSMTPSender(cfg SMTPConfig) *SMTPSender {
	return &SMTPSender{cfg: cfg}
}

func CheckSMTPConfig(cfg SMTPConfig) SMTPCheckReport {
	report := SMTPCheckReport{
		OK:       true,
		Enabled:  cfg.Enabled,
		Host:     cfg.Host,
		Port:     cfg.Port,
		From:     cfg.From,
		Security: cfg.Security,
		Timeout:  cfg.Timeout.String(),
		Auth:     "none",
	}
	if cfg.Username != "" || cfg.Password != "" {
		report.Auth = "plain"
	}
	if !cfg.Enabled {
		report.OK = false
		report.Errors = append(report.Errors, "smtp is disabled")
		return report
	}
	if strings.TrimSpace(cfg.Host) == "" {
		report.Errors = append(report.Errors, "smtp host is required")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		report.Errors = append(report.Errors, "smtp port must be within 1-65535")
	}
	if strings.TrimSpace(cfg.From) == "" {
		report.Errors = append(report.Errors, "smtp from is required")
	} else if _, err := mail.ParseAddress(cfg.From); err != nil {
		report.Errors = append(report.Errors, "smtp from must be a valid email address")
	}
	switch cfg.Security {
	case "none":
		report.Warnings = append(report.Warnings, "smtp tls is disabled")
	case "starttls", "tls":
	default:
		report.Errors = append(report.Errors, "smtp security must be one of none, starttls, tls")
	}
	if cfg.Password != "" && cfg.Username == "" {
		report.Warnings = append(report.Warnings, "smtp password is configured without username")
	}
	if cfg.Timeout <= 0 {
		report.Errors = append(report.Errors, "smtp timeout must be positive")
	}
	report.OK = len(report.Errors) == 0
	return report
}

func (s *SMTPSender) Send(ctx context.Context, message Message) error {
	check := CheckSMTPConfig(s.cfg)
	if !check.OK {
		return errors.New(strings.Join(check.Errors, "; "))
	}
	if len(message.To) == 0 {
		return errors.New("email recipient is required")
	}
	from := firstNonEmpty(message.From, s.cfg.From)
	if _, err := mail.ParseAddress(from); err != nil {
		return fmt.Errorf("invalid email from: %w", err)
	}
	recipients := make([]string, 0, len(message.To))
	for _, recipient := range message.To {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" {
			continue
		}
		parsed, err := mail.ParseAddress(recipient)
		if err != nil {
			return fmt.Errorf("invalid email recipient: %w", err)
		}
		recipients = append(recipients, parsed.Address)
	}
	if len(recipients) == 0 {
		return errors.New("email recipient is required")
	}

	client, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if s.cfg.Security == "starttls" {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return errors.New("smtp server does not advertise STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("smtp rcpt: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write([]byte(renderMessage(from, recipients, message))); err != nil {
		_ = writer.Close()
		return fmt.Errorf("smtp write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return client.Quit()
}

func (s *SMTPSender) connect(ctx context.Context) (*smtp.Client, error) {
	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprintf("%d", s.cfg.Port))
	dialer := net.Dialer{Timeout: s.cfg.Timeout}
	if s.cfg.Security == "tls" {
		conn, err := tls.DialWithDialer(&dialer, "tcp", addr, &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return nil, fmt.Errorf("smtp tls dial: %w", err)
		}
		_ = conn.SetDeadline(time.Now().Add(s.cfg.Timeout))
		return smtp.NewClient(conn, s.cfg.Host)
	}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("smtp dial: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(s.cfg.Timeout))
	return smtp.NewClient(conn, s.cfg.Host)
}

func renderMessage(from string, to []string, message Message) string {
	subject := strings.ReplaceAll(strings.TrimSpace(message.Subject), "\r", "")
	subject = strings.ReplaceAll(subject, "\n", " ")
	if subject == "" {
		subject = "ChatAPI test email"
	}
	body := strings.TrimSpace(message.Text)
	if body == "" {
		body = "ChatAPI SMTP test email."
	}
	headers := []string{
		"From: " + from,
		"To: " + strings.Join(to, ", "),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
	}
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + body + "\r\n"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
