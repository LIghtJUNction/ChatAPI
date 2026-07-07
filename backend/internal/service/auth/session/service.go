package session

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/service/auth/principal"
)

var (
	ErrMissingSecret   = errors.New("session secret is required")
	ErrInvalidSession  = errors.New("invalid session")
	ErrExpiredSession  = errors.New("expired session")
	ErrMissingCookie   = errors.New("missing session cookie")
	ErrUnsupportedKind = errors.New("unsupported principal kind")
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

func (s *Service) CookieName() string {
	return s.cookieName
}

func (s *Service) IssueToken(p principal.Principal) (Claims, string, error) {
	if p.Kind != principal.KindHumanSession {
		return Claims{}, "", ErrUnsupportedKind
	}
	if !p.Valid() {
		return Claims{}, "", ErrInvalidSession
	}
	now := s.now()
	claims := Claims{
		SessionID:  p.SubjectID,
		UserID:     p.UserID,
		Username:   p.Username,
		Role:       p.Role,
		IsAdmin:    p.IsAdmin,
		Source:     firstNonEmpty(p.Source, "session"),
		EntryPoint: firstNonEmpty(p.EntryPoint, "web"),
		AuthMethod: p.AuthMethod,
		IssuedAt:   now,
		ExpiresAt:  now.Add(s.ttl),
	}
	token, err := s.signClaims(claims)
	if err != nil {
		return Claims{}, "", err
	}
	return claims, token, nil
}

func (s *Service) IssueCookie(w http.ResponseWriter, p principal.Principal) (Claims, error) {
	claims, token, err := s.IssueToken(p)
	if err != nil {
		return Claims{}, err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    token,
		Path:     s.path,
		Expires:  claims.ExpiresAt,
		HttpOnly: s.httpOnly,
		Secure:   s.secureOnly,
		SameSite: s.sameSite,
	})
	return claims, nil
}

func (s *Service) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     s.path,
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: s.httpOnly,
		Secure:   s.secureOnly,
		SameSite: s.sameSite,
	})
}

func (s *Service) PrincipalFromRequest(r *http.Request) (principal.Principal, Claims, error) {
	cookie, err := r.Cookie(s.cookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return principal.Principal{}, Claims{}, ErrMissingCookie
		}
		return principal.Principal{}, Claims{}, err
	}
	claims, err := s.ParseToken(cookie.Value)
	if err != nil {
		return principal.Principal{}, Claims{}, err
	}
	return claims.Principal(), claims, nil
}

func (s *Service) ParseToken(raw string) (Claims, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 2 {
		return Claims{}, ErrInvalidSession
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalidSession
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidSession
	}
	expected := s.computeMAC(payload)
	if subtle.ConstantTimeCompare(signature, expected) != 1 {
		return Claims{}, ErrInvalidSession
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrInvalidSession
	}
	if strings.TrimSpace(claims.SessionID) == "" || strings.TrimSpace(claims.UserID) == "" {
		return Claims{}, ErrInvalidSession
	}
	if !claims.ExpiresAt.After(s.now()) {
		return Claims{}, ErrExpiredSession
	}
	return claims, nil
}

func (s *Service) NewSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "sess_" + hex.EncodeToString(raw[:]), nil
}

func (c Claims) Principal() principal.Principal {
	return principal.Principal{
		Kind:       principal.KindHumanSession,
		SubjectID:  strings.TrimSpace(c.SessionID),
		UserID:     strings.TrimSpace(c.UserID),
		Username:   strings.TrimSpace(c.Username),
		Role:       strings.TrimSpace(c.Role),
		IsAdmin:    c.IsAdmin,
		Source:     firstNonEmpty(c.Source, "session"),
		EntryPoint: firstNonEmpty(c.EntryPoint, "web"),
		AuthMethod: strings.TrimSpace(c.AuthMethod),
	}
}

func ContextWithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(Claims)
	return claims, ok
}

type claimsContextKey struct{}

func (s *Service) signClaims(claims Claims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signature := s.computeMAC(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (s *Service) computeMAC(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
