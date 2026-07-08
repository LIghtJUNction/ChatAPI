package access

import (
	"net/http"
	"strings"
	"time"
)

func (s *Service) AllowRequest(r *http.Request) bool {
	if s == nil || s.anonymousLimiter == nil || r == nil {
		return true
	}
	if isLabPublicPath(r) {
		return true
	}
	return s.anonymousLimiter.Allow(accessRateLimitKey(r))
}

func newRequestLimiter(max int, window time.Duration) *requestLimiter {
	if max <= 0 || window <= 0 {
		return nil
	}
	return &requestLimiter{
		now:      func() time.Time { return time.Now().UTC() },
		max:      max,
		window:   window,
		requests: map[string][]time.Time{},
	}
}

func (l *requestLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)
	items := l.requests[key][:0]
	for _, ts := range l.requests[key] {
		if ts.After(cutoff) {
			items = append(items, ts)
		}
	}
	if len(items) >= l.max {
		l.requests[key] = items
		return false
	}
	items = append(items, now)
	l.requests[key] = items
	return true
}
