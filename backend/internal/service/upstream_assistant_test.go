package service

import (
	"testing"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

func TestBuildUpstreamAssistantSchema(t *testing.T) {
	schema := BuildUpstreamAssistantSchema()
	if len(schema.ProtocolOptions) != 3 {
		t.Fatalf("unexpected protocol options: %#v", schema)
	}
	if len(schema.Fields) == 0 || schema.Fields[0].Key != "enabled" {
		t.Fatalf("unexpected upstream assistant schema fields: %#v", schema)
	}
	if len(schema.SensitiveFields) != 1 || schema.SensitiveFields[0] != "api_key" {
		t.Fatalf("unexpected sensitive fields: %#v", schema)
	}
}

func TestBuildUpstreamAssistantHintsDetectsRecursiveBaseURL(t *testing.T) {
	cfg := config.Config{Host: "0.0.0.0", Port: 5000, BaseURL: "https://chatapi.example.com"}
	hints := BuildUpstreamAssistantHints(cfg, "", "https://chatapi.example.com/v1")
	if !hints.CandidateRecursive {
		t.Fatalf("expected recursive candidate: %#v", hints)
	}
	if len(hints.CurrentInstanceURLs) == 0 {
		t.Fatalf("expected current instance urls: %#v", hints)
	}
	if len(hints.Warnings) == 0 {
		t.Fatalf("expected recursive warning: %#v", hints)
	}
}

func TestBuildUpstreamAssistantHintsAllowsDifferentBaseURL(t *testing.T) {
	cfg := config.Config{Host: "127.0.0.1", Port: 5000}
	hints := BuildUpstreamAssistantHints(cfg, "", "http://127.0.0.1:11434")
	if hints.CandidateRecursive {
		t.Fatalf("expected non-recursive candidate: %#v", hints)
	}
	if len(hints.Warnings) != 0 {
		t.Fatalf("unexpected warnings for non-recursive candidate: %#v", hints)
	}
}

func TestBuildUpstreamAssistantHintsUsesObservedBaseURL(t *testing.T) {
	cfg := config.Config{Host: "0.0.0.0", Port: 0}
	hints := BuildUpstreamAssistantHints(cfg, "http://127.0.0.1:39123", "http://127.0.0.1:39123/v1")
	if !hints.CandidateRecursive {
		t.Fatalf("expected recursive candidate via observed base url: %#v", hints)
	}
}

func TestBuildUpstreamInputHintsTruncatesNewestWindow(t *testing.T) {
	messages := []store.Message{
		{ID: "msg1", Role: "user", Content: "one"},
		{ID: "msg2", Role: "assistant", Content: "two"},
		{ID: "msg3", Role: "user", Content: "three"},
	}
	hints := BuildUpstreamInputHints(messages, "draft", 2)
	if !hints.Truncated || hints.ExcludedMessages != 1 {
		t.Fatalf("expected truncation metadata: %#v", hints)
	}
	if len(hints.RecommendedMessages) != 2 {
		t.Fatalf("unexpected recommended messages: %#v", hints)
	}
	if hints.RecommendedMessages[0]["id"] != "msg2" || hints.RecommendedMessages[1]["id"] != "msg3" {
		t.Fatalf("unexpected recommended window order: %#v", hints.RecommendedMessages)
	}
	if len(hints.ConstructionRules) < 3 {
		t.Fatalf("expected construction rules: %#v", hints)
	}
}
