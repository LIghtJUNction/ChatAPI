package service

import (
	"strings"
	"testing"
	"time"
)

func TestSessionCodecRoundTrip(t *testing.T) {
	codec := NewSessionCodec("test-secret")
	raw, err := codec.Encode(RequestActor{
		UserID:   "admin",
		Username: "admin",
		Role:     "admin",
		Source:   "session",
	}, time.Hour)
	if err != nil {
		t.Fatalf("encode session: %v", err)
	}
	actor, err := codec.Decode(raw)
	if err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if actor.UserID != "admin" || actor.Username != "admin" || actor.Role != "admin" || actor.Source != "session" {
		t.Fatalf("unexpected actor: %#v", actor)
	}
}

func TestSessionCodecRejectsTamperedToken(t *testing.T) {
	codec := NewSessionCodec("test-secret")
	raw, err := codec.Encode(RequestActor{
		UserID:   "admin",
		Username: "admin",
		Role:     "admin",
		Source:   "session",
	}, time.Hour)
	if err != nil {
		t.Fatalf("encode session: %v", err)
	}
	raw = strings.Replace(raw, ".", "x.", 1)
	if _, err := codec.Decode(raw); err == nil {
		t.Fatalf("expected tampered session to be rejected")
	}
}

func TestSessionCodecRejectsExpiredToken(t *testing.T) {
	now := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	codec := NewSessionCodec("test-secret")
	codec.now = func() time.Time { return now }
	raw, err := codec.Encode(RequestActor{
		UserID:   "admin",
		Username: "admin",
		Role:     "admin",
		Source:   "session",
	}, time.Second)
	if err != nil {
		t.Fatalf("encode session: %v", err)
	}
	codec.now = func() time.Time { return now.Add(2 * time.Second) }
	if _, err := codec.Decode(raw); err == nil {
		t.Fatalf("expected expired session to be rejected")
	}
}
