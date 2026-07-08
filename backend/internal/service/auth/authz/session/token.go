package session

import (
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

	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/principal"
)

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
