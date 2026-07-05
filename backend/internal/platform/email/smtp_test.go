package email

import (
	"strings"
	"testing"
	"time"
)

func TestCheckSMTPConfigRejectsDisabledAndMissingRequiredFields(t *testing.T) {
	disabled := CheckSMTPConfig(SMTPConfig{})
	if disabled.OK || !containsString(disabled.Errors, "smtp is disabled") {
		t.Fatalf("expected disabled smtp error: %#v", disabled)
	}

	report := CheckSMTPConfig(SMTPConfig{
		Enabled:  true,
		Port:     70000,
		Security: "bad",
		Timeout:  0,
	})
	for _, expected := range []string{
		"smtp host is required",
		"smtp port must be within 1-65535",
		"smtp from is required",
		"smtp security must be one of none, starttls, tls",
		"smtp timeout must be positive",
	} {
		if !containsString(report.Errors, expected) {
			t.Fatalf("missing expected error %q in %#v", expected, report)
		}
	}
}

func TestCheckSMTPConfigAcceptsStartTLS(t *testing.T) {
	report := CheckSMTPConfig(SMTPConfig{
		Enabled:  true,
		Host:     "smtp.example.com",
		Port:     587,
		From:     "ChatAPI <noreply@example.com>",
		Security: "starttls",
		Timeout:  10 * time.Second,
	})
	if !report.OK || len(report.Errors) != 0 {
		t.Fatalf("unexpected smtp check report: %#v", report)
	}
	if report.Auth != "none" || report.Timeout != "10s" {
		t.Fatalf("unexpected smtp check metadata: %#v", report)
	}
}

func TestCheckSMTPConfigWarnsForPlainSMTP(t *testing.T) {
	report := CheckSMTPConfig(SMTPConfig{
		Enabled:  true,
		Host:     "smtp.local",
		Port:     25,
		From:     "noreply@example.com",
		Security: "none",
		Timeout:  time.Second,
	})
	if !report.OK || !containsString(report.Warnings, "smtp tls is disabled") {
		t.Fatalf("expected plain smtp warning: %#v", report)
	}
}

func TestRenderMessageSanitizesSubjectHeader(t *testing.T) {
	raw := renderMessage("noreply@example.com", []string{"user@example.com"}, Message{
		Subject: "hello\r\nBcc: victim@example.com",
		Text:    "body",
	})
	if strings.Contains(raw, "\r\nBcc:") {
		t.Fatalf("subject header injection was not sanitized:\n%s", raw)
	}
	if !strings.Contains(raw, "Subject: hello Bcc: victim@example.com") {
		t.Fatalf("unexpected rendered subject:\n%s", raw)
	}
}

func containsString(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}
