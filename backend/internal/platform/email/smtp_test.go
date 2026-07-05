package email

import (
	"bufio"
	"context"
	"errors"
	"net"
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

func TestCheckConnectionDoesNotSendMailTransaction(t *testing.T) {
	addr, done := startFakeSMTPServer(t)
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split fake smtp addr: %v", err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatalf("parse fake smtp port: %v", err)
	}

	sender := NewSMTPSender(SMTPConfig{
		Enabled:  true,
		Host:     host,
		Port:     port,
		From:     "noreply@example.com",
		Security: "none",
		Timeout:  2 * time.Second,
	})
	if err := sender.CheckConnection(context.Background()); err != nil {
		t.Fatalf("check connection: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("fake smtp server observed unexpected command: %v", err)
	}
}

func startFakeSMTPServer(t *testing.T) (string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake smtp: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		if _, err := conn.Write([]byte("220 fake smtp\r\n")); err != nil {
			done <- err
			return
		}
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "MAIL "):
				done <- errors.New("unexpected MAIL command")
				return
			case strings.HasPrefix(upper, "QUIT"):
				_, err := conn.Write([]byte("221 bye\r\n"))
				done <- err
				return
			default:
				_, err := conn.Write([]byte("250 ok\r\n"))
				if err != nil {
					done <- err
					return
				}
			}
		}
		done <- scanner.Err()
	}()
	return listener.Addr().String(), done
}

func containsString(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}
