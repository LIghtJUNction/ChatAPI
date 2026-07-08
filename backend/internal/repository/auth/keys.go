package auth

import "time"

type AppAPIKey struct {
	ID             string         `json:"id"`
	UserID         string         `json:"user_id"`
	Name           string         `json:"name"`
	KeyHash        string         `json:"-"`
	KeyPrefix      string         `json:"key_prefix"`
	Scopes         []string       `json:"scopes,omitempty"`
	ResourceLimits map[string]any `json:"resource_limits,omitempty"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
	LastUsedAt     *time.Time     `json:"last_used_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	RevokedAt      *time.Time     `json:"revoked_at,omitempty"`
}

type AppAPIKeyAuditLog struct {
	ID          string    `json:"id"`
	AppAPIKeyID string    `json:"app_api_key_id"`
	UserID      string    `json:"user_id"`
	Route       string    `json:"route"`
	StatusCode  int       `json:"status_code"`
	ErrorCode   string    `json:"error_code,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type ListAppAPIKeyAuditLogsInput struct {
	Limit  int
	UserID string
}

type CreateAppAPIKeyInput struct {
	ID             string
	UserID         string
	Name           string
	KeyHash        string
	KeyPrefix      string
	Scopes         []string
	ResourceLimits map[string]any
	ExpiresAt      *time.Time
}

type CreateModelAPIKeyInput struct {
	ID            string
	UserID        string
	Name          string
	KeyCiphertext string
	KeyPrefix     string
	Model         string
}
