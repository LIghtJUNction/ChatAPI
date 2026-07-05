package app

import "testing"

func TestParseSMTPTestOptionsConnectOnly(t *testing.T) {
	options, err := parseSMTPTestOptions([]string{"--connect-only", "--to", "user@example.com", "--subject", "hello"})
	if err != nil {
		t.Fatalf("parse smtp options: %v", err)
	}
	if !options.connectOnly || options.dryRun {
		t.Fatalf("unexpected mode flags: %#v", options)
	}
	if options.to != "user@example.com" || options.subject != "hello" {
		t.Fatalf("unexpected smtp options: %#v", options)
	}
}

func TestParseSMTPTestOptionsRejectsDryRunWithConnectOnly(t *testing.T) {
	_, err := parseSMTPTestOptions([]string{"--dry-run", "--connect-only"})
	if err == nil {
		t.Fatal("expected incompatible smtp options error")
	}
}
