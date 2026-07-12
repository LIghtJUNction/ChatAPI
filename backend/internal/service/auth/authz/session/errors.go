package session

import "errors"

var (
	ErrMissingSecret   = errors.New("session secret is required")
	ErrInvalidSession  = errors.New("invalid session")
	ErrExpiredSession  = errors.New("expired session")
	ErrMissingCookie   = errors.New("missing session cookie")
	ErrUnsupportedKind = errors.New("unsupported principal kind")
)
