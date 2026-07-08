package postgresql

import (
	"context"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

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

func (s *Store) GetAuthVerificationCode(ctx context.Context, email string, purpose string) (store.AuthVerificationCode, error) {
	return scanAuthVerificationCode(s.pool.QueryRow(ctx, `
		SELECT email, purpose, code_hash, failed_attempts, expires_at, last_sent_at, created_at, updated_at
		FROM auth_verification_codes
		WHERE email = $1 AND purpose = $2
	`, strings.TrimSpace(strings.ToLower(email)), strings.TrimSpace(purpose)))
}

func (s *Store) UpsertAuthVerificationCode(ctx context.Context, input store.UpsertAuthVerificationCodeInput) (store.AuthVerificationCode, error) {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO auth_verification_codes(email, purpose, code_hash, failed_attempts, expires_at, last_sent_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(email, purpose) DO UPDATE SET
			code_hash = excluded.code_hash,
			failed_attempts = excluded.failed_attempts,
			expires_at = excluded.expires_at,
			last_sent_at = excluded.last_sent_at,
			updated_at = excluded.updated_at
	`, strings.TrimSpace(strings.ToLower(input.Email)), strings.TrimSpace(input.Purpose), strings.TrimSpace(input.CodeHash), input.FailedAttempts, input.ExpiresAt.UTC(), input.LastSentAt.UTC(), now, now)
	if err != nil {
		return store.AuthVerificationCode{}, err
	}
	return s.GetAuthVerificationCode(ctx, input.Email, input.Purpose)
}

func (s *Store) DeleteAuthVerificationCode(ctx context.Context, email string, purpose string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM auth_verification_codes
		WHERE email = $1 AND purpose = $2
	`, strings.TrimSpace(strings.ToLower(email)), strings.TrimSpace(purpose))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteExpiredAuthVerificationCodes(ctx context.Context, before time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM auth_verification_codes
		WHERE expires_at <= $1
	`, before.UTC())
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
