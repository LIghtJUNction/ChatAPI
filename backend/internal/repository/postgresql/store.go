package postgresql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zyf/chatapi/internal/store"
)

type Store struct {
	pool *pgxpool.Pool
}

var errNotImplemented = errors.New("postgresql repository method not implemented")

func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgresql pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgresql: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func Bootstrap(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'user',
			is_active BOOLEAN NOT NULL DEFAULT true,
			local_admin BOOLEAN NOT NULL DEFAULT false,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			last_login_at TIMESTAMPTZ NULL
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_nonempty ON users(username) WHERE username <> '';
		CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_nonempty ON users(email) WHERE email <> '';

		CREATE TABLE IF NOT EXISTS user_identities (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			provider TEXT NOT NULL,
			subject TEXT NOT NULL,
			email TEXT NOT NULL DEFAULT '',
			email_verified BOOLEAN NOT NULL DEFAULT false,
			profile_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			last_login_at TIMESTAMPTZ NULL,
			UNIQUE(provider, subject)
		);
		CREATE INDEX IF NOT EXISTS idx_user_identities_user_id ON user_identities(user_id);

		CREATE TABLE IF NOT EXISTS config (
			key TEXT PRIMARY KEY,
			value_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		);

		CREATE TABLE IF NOT EXISTS user_configs (
			user_id TEXT NOT NULL,
			key TEXT NOT NULL,
			value_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY(user_id, key)
		);

		CREATE TABLE IF NOT EXISTS user_app_api_keys (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			key_hash TEXT NOT NULL,
			key_prefix TEXT NOT NULL UNIQUE,
			scopes_json JSONB NOT NULL DEFAULT '[]'::jsonb,
			resource_limits_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			expires_at TIMESTAMPTZ NULL,
			last_used_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL,
			revoked_at TIMESTAMPTZ NULL
		);
		CREATE INDEX IF NOT EXISTS idx_user_app_api_keys_user_id ON user_app_api_keys(user_id);

		CREATE TABLE IF NOT EXISTS app_api_key_audit_logs (
			id TEXT PRIMARY KEY,
			app_api_key_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			route TEXT NOT NULL DEFAULT '',
			status_code INTEGER NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_app_api_key_audit_logs_user_created ON app_api_key_audit_logs(user_id, created_at DESC);

		CREATE TABLE IF NOT EXISTS user_api_keys (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			key_ciphertext TEXT NOT NULL,
			key_prefix TEXT NOT NULL UNIQUE,
			model TEXT NOT NULL DEFAULT '',
			last_used_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL,
			revoked_at TIMESTAMPTZ NULL
		);
		CREATE INDEX IF NOT EXISTS idx_user_api_keys_user_id ON user_api_keys(user_id);
	`)
	if err != nil {
		return fmt.Errorf("bootstrap postgresql schema: %w", err)
	}
	return nil
}

func (s *Store) CreateUser(ctx context.Context, input store.CreateUserInput) (store.User, error) {
	now := time.Now().UTC()
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "user"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users(id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL)
	`,
		input.ID,
		strings.TrimSpace(input.Username),
		strings.TrimSpace(input.Email),
		input.PasswordHash,
		role,
		input.IsActive,
		input.LocalAdmin,
		now,
		now,
	)
	if err != nil {
		return store.User{}, err
	}
	return store.User{
		ID:           input.ID,
		Username:     strings.TrimSpace(input.Username),
		Email:        strings.TrimSpace(input.Email),
		PasswordHash: input.PasswordHash,
		Role:         role,
		IsActive:     input.IsActive,
		LocalAdmin:   input.LocalAdmin,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (s *Store) UpdateUser(ctx context.Context, input store.UpdateUserInput) (store.User, error) {
	now := time.Now().UTC()
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "user"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE users
		SET username = $1, email = $2, password_hash = $3, role = $4, is_active = $5, local_admin = $6, updated_at = $7, last_login_at = $8
		WHERE id = $9
	`,
		strings.TrimSpace(input.Username),
		strings.TrimSpace(input.Email),
		input.PasswordHash,
		role,
		input.IsActive,
		input.LocalAdmin,
		now,
		input.LastLoginAt,
		input.ID,
	)
	if err != nil {
		return store.User{}, err
	}
	if tag.RowsAffected() == 0 {
		return store.User{}, store.ErrNotFound
	}
	return s.GetUser(ctx, input.ID)
}

func (s *Store) GetUser(ctx context.Context, id string) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE id = $1
	`, id))
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE email = $1
	`, strings.TrimSpace(email)))
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE username = $1
	`, strings.TrimSpace(username)))
}

func (s *Store) ListUsers(ctx context.Context) ([]store.User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.User, 0)
	for rows.Next() {
		item, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertUserIdentity(ctx context.Context, input store.UpsertUserIdentityInput) (store.UserIdentity, error) {
	now := time.Now().UTC()
	profileJSON := mustJSON(ensureMap(input.Profile))
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_identities(id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
		ON CONFLICT(provider, subject) DO UPDATE SET
			user_id = excluded.user_id,
			email = excluded.email,
			email_verified = excluded.email_verified,
			profile_json = excluded.profile_json,
			updated_at = excluded.updated_at,
			last_login_at = excluded.last_login_at
	`,
		input.ID,
		input.UserID,
		strings.TrimSpace(input.Provider),
		strings.TrimSpace(input.Subject),
		strings.TrimSpace(input.Email),
		input.EmailVerified,
		profileJSON,
		now,
		now,
		input.LastLoginAt,
	)
	if err != nil {
		return store.UserIdentity{}, err
	}
	return s.GetUserIdentity(ctx, strings.TrimSpace(input.Provider), strings.TrimSpace(input.Subject))
}

func (s *Store) GetUserIdentity(ctx context.Context, provider string, subject string) (store.UserIdentity, error) {
	return scanUserIdentity(s.pool.QueryRow(ctx, `
		SELECT id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at
		FROM user_identities
		WHERE provider = $1 AND subject = $2
	`, strings.TrimSpace(provider), strings.TrimSpace(subject)))
}

func (s *Store) ListUserIdentities(ctx context.Context, userID string) ([]store.UserIdentity, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at
		FROM user_identities
		WHERE user_id = $1
		ORDER BY provider ASC, created_at DESC, id DESC
	`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.UserIdentity, 0)
	for rows.Next() {
		item, err := scanUserIdentity(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteUserIdentity(ctx context.Context, id string, userID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM user_identities
		WHERE id = $1 AND user_id = $2
	`, strings.TrimSpace(id), strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetSystemConfig(ctx context.Context, key string) (store.SystemConfig, error) {
	return scanSystemConfig(s.pool.QueryRow(ctx, `
		SELECT key, value_json, created_at, updated_at
		FROM config
		WHERE key = $1
	`, strings.TrimSpace(key)))
}

func (s *Store) SetSystemConfig(ctx context.Context, input store.SetSystemConfigInput) (store.SystemConfig, error) {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO config(key, value_json, created_at, updated_at)
		VALUES ($1, $2::jsonb, $3, $4)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, strings.TrimSpace(input.Key), mustJSON(ensureMap(input.Value)), now, now)
	if err != nil {
		return store.SystemConfig{}, err
	}
	return s.GetSystemConfig(ctx, input.Key)
}

func (s *Store) DeleteSystemConfig(ctx context.Context, key string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM config WHERE key = $1`, strings.TrimSpace(key))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListSystemConfigs(ctx context.Context) ([]store.SystemConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT key, value_json, created_at, updated_at
		FROM config
		ORDER BY key ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.SystemConfig, 0)
	for rows.Next() {
		item, err := scanSystemConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetUserConfig(ctx context.Context, userID string, key string) (store.UserConfig, error) {
	return scanUserConfig(s.pool.QueryRow(ctx, `
		SELECT user_id, key, value_json, created_at, updated_at
		FROM user_configs
		WHERE user_id = $1 AND key = $2
	`, strings.TrimSpace(userID), strings.TrimSpace(key)))
}

func (s *Store) SetUserConfig(ctx context.Context, input store.SetUserConfigInput) (store.UserConfig, error) {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_configs(user_id, key, value_json, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, $4, $5)
		ON CONFLICT(user_id, key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, strings.TrimSpace(input.UserID), strings.TrimSpace(input.Key), mustJSON(ensureMap(input.Value)), now, now)
	if err != nil {
		return store.UserConfig{}, err
	}
	return s.GetUserConfig(ctx, input.UserID, input.Key)
}

func (s *Store) DeleteUserConfig(ctx context.Context, userID string, key string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM user_configs
		WHERE user_id = $1 AND key = $2
	`, strings.TrimSpace(userID), strings.TrimSpace(key))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListUserConfigs(ctx context.Context, userID string) ([]store.UserConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, key, value_json, created_at, updated_at
		FROM user_configs
		WHERE user_id = $1
		ORDER BY key ASC
	`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.UserConfig, 0)
	for rows.Next() {
		item, err := scanUserConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (store.User, error) {
	var item store.User
	var lastLoginAt *time.Time
	if err := row.Scan(
		&item.ID,
		&item.Username,
		&item.Email,
		&item.PasswordHash,
		&item.Role,
		&item.IsActive,
		&item.LocalAdmin,
		&item.CreatedAt,
		&item.UpdatedAt,
		&lastLoginAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.User{}, store.ErrNotFound
		}
		return store.User{}, err
	}
	item.LastLoginAt = lastLoginAt
	return item, nil
}

func scanUserIdentity(row rowScanner) (store.UserIdentity, error) {
	var item store.UserIdentity
	var profileJSON []byte
	var lastLoginAt *time.Time
	if err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.Provider,
		&item.Subject,
		&item.Email,
		&item.EmailVerified,
		&profileJSON,
		&item.CreatedAt,
		&item.UpdatedAt,
		&lastLoginAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.UserIdentity{}, store.ErrNotFound
		}
		return store.UserIdentity{}, err
	}
	item.Profile = parseJSONMap(profileJSON)
	item.LastLoginAt = lastLoginAt
	return item, nil
}

func scanSystemConfig(row rowScanner) (store.SystemConfig, error) {
	var item store.SystemConfig
	var valueJSON []byte
	if err := row.Scan(&item.Key, &valueJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.SystemConfig{}, store.ErrNotFound
		}
		return store.SystemConfig{}, err
	}
	item.Value = parseJSONMap(valueJSON)
	return item, nil
}

func scanUserConfig(row rowScanner) (store.UserConfig, error) {
	var item store.UserConfig
	var valueJSON []byte
	if err := row.Scan(&item.UserID, &item.Key, &valueJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.UserConfig{}, store.ErrNotFound
		}
		return store.UserConfig{}, err
	}
	item.Value = parseJSONMap(valueJSON)
	return item, nil
}

func ensureMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func parseJSONMap(data []byte) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil || result == nil {
		return map[string]any{}
	}
	return result
}
