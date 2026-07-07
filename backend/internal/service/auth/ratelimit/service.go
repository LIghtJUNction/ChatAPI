package ratelimit

import (
	"strings"
	"sync"
	"time"
)

type Service struct {
	mu          sync.Mutex
	now         func() time.Time
	maxFailures int
	lockout     time.Duration
	attempts    map[string]attempt
}

type attempt struct {
	Failures  int
	LockedTil time.Time
}

func NewService(maxFailures int, lockout time.Duration) *Service {
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if lockout <= 0 {
		lockout = time.Minute
	}
	return &Service{
		now:         func() time.Time { return time.Now().UTC() },
		maxFailures: maxFailures,
		lockout:     lockout,
		attempts:    map[string]attempt{},
	}
}

func (s *Service) Allow(key string) bool {
	if s == nil {
		return true
	}
	key = normalizeKey(key)
	if key == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.attempts[key]
	now := s.now()
	if !item.LockedTil.IsZero() && item.LockedTil.After(now) {
		return false
	}
	if !item.LockedTil.IsZero() && !item.LockedTil.After(now) {
		delete(s.attempts, key)
	}
	return true
}

func (s *Service) RecordFailure(key string) {
	if s == nil {
		return
	}
	key = normalizeKey(key)
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.attempts[key]
	now := s.now()
	item.Failures++
	if item.Failures >= s.maxFailures {
		item.LockedTil = now.Add(s.lockout)
	}
	s.attempts[key] = item
}

func (s *Service) Reset(key string) {
	if s == nil {
		return
	}
	key = normalizeKey(key)
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, key)
}

func normalizeKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}
