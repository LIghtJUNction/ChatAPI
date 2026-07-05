package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const SessionCookieName = "chatapi_session"
const DefaultSessionTTL = 24 * time.Hour

var ErrInvalidSession = errors.New("invalid session")

type SessionCodec struct {
	secret []byte
	now    func() time.Time
}

type SessionClaims struct {
	UserID    string `json:"uid"`
	Username  string `json:"un"`
	Role      string `json:"role"`
	Source    string `json:"src"`
	ExpiresAt int64  `json:"exp"`
}

func NewSessionCodec(secret string) *SessionCodec {
	return &SessionCodec{
		secret: []byte(strings.TrimSpace(secret)),
		now:    time.Now,
	}
}

func (c *SessionCodec) Encode(actor RequestActor, ttl time.Duration) (string, error) {
	if c == nil || len(c.secret) == 0 {
		return "", ErrInvalidSession
	}
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	claims := SessionClaims{
		UserID:    strings.TrimSpace(actor.UserID),
		Username:  strings.TrimSpace(actor.Username),
		Role:      strings.TrimSpace(actor.Role),
		Source:    strings.TrimSpace(actor.Source),
		ExpiresAt: c.now().UTC().Add(ttl).Unix(),
	}
	if claims.UserID == "" || claims.Username == "" || claims.Role == "" {
		return "", ErrInvalidSession
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := c.sign(encodedPayload)
	return encodedPayload + "." + signature, nil
}

func (c *SessionCodec) Decode(raw string) (RequestActor, error) {
	if c == nil || len(c.secret) == 0 {
		return RequestActor{}, ErrInvalidSession
	}
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return RequestActor{}, ErrInvalidSession
	}
	if !hmac.Equal([]byte(parts[1]), []byte(c.sign(parts[0]))) {
		return RequestActor{}, ErrInvalidSession
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return RequestActor{}, ErrInvalidSession
	}
	var claims SessionClaims
	if err := json.Unmarshal(data, &claims); err != nil {
		return RequestActor{}, ErrInvalidSession
	}
	if claims.ExpiresAt <= c.now().UTC().Unix() {
		return RequestActor{}, ErrInvalidSession
	}
	actor := RequestActor{
		UserID:   strings.TrimSpace(claims.UserID),
		Username: strings.TrimSpace(claims.Username),
		Role:     strings.TrimSpace(claims.Role),
		Source:   strings.TrimSpace(claims.Source),
	}
	if actor.UserID == "" || actor.Username == "" || actor.Role == "" {
		return RequestActor{}, ErrInvalidSession
	}
	if actor.Source == "" {
		actor.Source = "session"
	}
	return actor, nil
}

func (c *SessionCodec) sign(payload string) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func SessionMaxAge(ttl time.Duration) int {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return int(ttl.Seconds())
}

func ExpiredSessionMaxAge() int {
	return -1
}
