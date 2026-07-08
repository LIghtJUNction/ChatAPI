package session

import (
	"net/http"
	"strings"
	"time"
)

func NewService(cfg Config) (*Service, error) {
	secret := strings.TrimSpace(cfg.Secret)
	if secret == "" {
		return nil, ErrMissingSecret
	}
	cookieName := strings.TrimSpace(cfg.CookieName)
	if cookieName == "" {
		cookieName = "chatapi_session"
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		path = "/"
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	sameSite := cfg.SameSite
	if sameSite == 0 {
		sameSite = http.SameSiteLaxMode
	}
	httpOnly := true
	if cfg.HTTPOnly != nil {
		httpOnly = *cfg.HTTPOnly
	}
	return &Service{
		secret:     []byte(secret),
		cookieName: cookieName,
		ttl:        ttl,
		secureOnly: cfg.SecureOnly,
		httpOnly:   httpOnly,
		sameSite:   sameSite,
		path:       path,
		now:        now,
	}, nil
}
