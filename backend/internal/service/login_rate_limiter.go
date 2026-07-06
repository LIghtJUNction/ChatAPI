package service

import (
	"strings"
	"sync"
	"time"
)

type LoginRateLimiter struct {
	mu          sync.Mutex
	now         func() time.Time
	maxFailures int
	lockout     time.Duration
	attempts    map[string]loginAttempt
}

type loginAttempt struct {
	Failures  int
	LockedTil time.Time
	UpdatedAt time.Time
}

func NewLoginRateLimiter(maxFailures int, lockout time.Duration) *LoginRateLimiter {
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if lockout <= 0 {
		lockout = time.Minute
	}
	return &LoginRateLimiter{
		now:         time.Now,
		maxFailures: maxFailures,
		lockout:     lockout,
		attempts:    make(map[string]loginAttempt),
	}
}

func (l *LoginRateLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	key = normalizeLoginLimitKey(key)
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	attempt := l.attempts[key]
	now := l.now().UTC()
	if !attempt.LockedTil.IsZero() && attempt.LockedTil.After(now) {
		return false
	}
	if !attempt.LockedTil.IsZero() && !attempt.LockedTil.After(now) {
		delete(l.attempts, key)
	}
	return true
}

func (l *LoginRateLimiter) RecordFailure(key string) {
	if l == nil {
		return
	}
	key = normalizeLoginLimitKey(key)
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now().UTC()
	attempt := l.attempts[key]
	if !attempt.LockedTil.IsZero() && attempt.LockedTil.After(now) {
		attempt.UpdatedAt = now
		l.attempts[key] = attempt
		return
	}
	attempt.Failures++
	attempt.UpdatedAt = now
	if attempt.Failures >= l.maxFailures {
		attempt.LockedTil = now.Add(l.lockout)
	}
	l.attempts[key] = attempt
}

func (l *LoginRateLimiter) Reset(key string) {
	if l == nil {
		return
	}
	key = normalizeLoginLimitKey(key)
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func normalizeLoginLimitKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}
