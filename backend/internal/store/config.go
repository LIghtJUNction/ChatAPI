package store

import "time"

type SystemConfig struct {
	Key       string         `json:"key"`
	Value     map[string]any `json:"value"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type UserConfig struct {
	UserID    string         `json:"user_id"`
	Key       string         `json:"key"`
	Value     map[string]any `json:"value"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type AuthVerificationCode struct {
	Email          string    `json:"email"`
	Purpose        string    `json:"purpose"`
	CodeHash       string    `json:"-"`
	FailedAttempts int       `json:"failed_attempts"`
	ExpiresAt      time.Time `json:"expires_at"`
	LastSentAt     time.Time `json:"last_sent_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AutomationRule struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	Enabled   bool           `json:"enabled"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
