package session

import (
	"net/http"
	"strings"
	"time"
)

type Config struct {
	Secret     string
	CookieName string
	TTL        time.Duration
	SecureOnly bool
	HTTPOnly   *bool
	SameSite   http.SameSite
	Path       string
	Now        func() time.Time
}

type Service struct {
	secret     []byte
	cookieName string
	ttl        time.Duration
	secureOnly bool
	httpOnly   bool
	sameSite   http.SameSite
	path       string
	now        func() time.Time
}

type Claims struct {
	SessionID  string    `json:"sid"`
	UserID     string    `json:"uid"`
	Username   string    `json:"username,omitempty"`
	Role       string    `json:"role,omitempty"`
	IsAdmin    bool      `json:"is_admin,omitempty"`
	Source     string    `json:"src,omitempty"`
	EntryPoint string    `json:"entry,omitempty"`
	AuthMethod string    `json:"auth_method,omitempty"`
	IssuedAt   time.Time `json:"iat"`
	ExpiresAt  time.Time `json:"exp"`
}

func (s *Service) CookieName() string {
	return s.cookieName
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
