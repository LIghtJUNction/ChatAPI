package service

import (
	"testing"
	"time"
)

func TestLoginRateLimiterLocksAndExpires(t *testing.T) {
	now := time.Date(2026, 7, 6, 1, 0, 0, 0, time.UTC)
	limiter := NewLoginRateLimiter(2, time.Minute)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow("admin|127.0.0.1") {
		t.Fatal("first attempt should be allowed")
	}
	limiter.RecordFailure("admin|127.0.0.1")
	if !limiter.Allow("admin|127.0.0.1") {
		t.Fatal("second attempt should still be allowed")
	}
	limiter.RecordFailure("admin|127.0.0.1")
	if limiter.Allow("admin|127.0.0.1") {
		t.Fatal("key should be locked after max failures")
	}

	now = now.Add(time.Minute + time.Second)
	if !limiter.Allow("admin|127.0.0.1") {
		t.Fatal("key should unlock after lockout")
	}
}

func TestLoginRateLimiterResetClearsFailures(t *testing.T) {
	limiter := NewLoginRateLimiter(2, time.Minute)
	key := "admin|127.0.0.1"

	limiter.RecordFailure(key)
	limiter.Reset(key)
	limiter.RecordFailure(key)
	if !limiter.Allow(key) {
		t.Fatal("reset should clear previous failures")
	}
}
