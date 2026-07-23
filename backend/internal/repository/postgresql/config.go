package postgresql

import (
	"context"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) GetSystemConfig(ctx context.Context, key string) (common.SystemConfig, error) {
	return scanSystemConfig(s.pool.QueryRow(ctx, `
		SELECT key, value_json, created_at, updated_at
		FROM config
		WHERE key = $1
	`, strings.TrimSpace(key)))
}

func (s *Store) SetSystemConfig(ctx context.Context, input common.SetSystemConfigInput) (common.SystemConfig, error) {
	now := time.Now().UTC()
	key, valueJSON := strings.TrimSpace(input.Key), mustJSON(ensureMap(input.Value))
	// System settings intentionally use last-write-wins. This project does not
	// coordinate concurrent administrator edits or reject a later submission.
	return scanSystemConfig(s.pool.QueryRow(ctx, `
		INSERT INTO config(key,value_json,created_at,updated_at)
		VALUES($1,$2::jsonb,$3,$4)
		ON CONFLICT(key) DO UPDATE SET
			value_json=excluded.value_json,
			updated_at=excluded.updated_at
		RETURNING key,value_json,created_at,updated_at
	`, key, valueJSON, now, now))
}

func (s *Store) SetSystemConfigs(ctx context.Context, inputs []common.SetSystemConfigInput) ([]common.SystemConfig, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	items := make([]common.SystemConfig, 0, len(inputs))
	for _, input := range inputs {
		now := time.Now().UTC()
		item, err := scanSystemConfig(tx.QueryRow(ctx, `
			INSERT INTO config(key,value_json,created_at,updated_at)
			VALUES($1,$2::jsonb,$3,$4)
			ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json,updated_at=excluded.updated_at
			RETURNING key,value_json,created_at,updated_at
		`, strings.TrimSpace(input.Key), mustJSON(ensureMap(input.Value)), now, now))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) DeleteSystemConfig(ctx context.Context, key string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM config WHERE key = $1`, strings.TrimSpace(key))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return common.ErrNotFound
	}
	return nil
}

func (s *Store) ListSystemConfigs(ctx context.Context) ([]common.SystemConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT key, value_json, created_at, updated_at
		FROM config
		ORDER BY key ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.SystemConfig, 0)
	for rows.Next() {
		item, err := scanSystemConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetUserConfig(ctx context.Context, userID string, key string) (common.UserConfig, error) {
	return scanUserConfig(s.pool.QueryRow(ctx, `
		SELECT user_id, key, value_json, created_at, updated_at
		FROM user_configs
		WHERE user_id = $1 AND key = $2
	`, strings.TrimSpace(userID), strings.TrimSpace(key)))
}

func (s *Store) SetUserConfig(ctx context.Context, input common.SetUserConfigInput) (common.UserConfig, error) {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_configs(user_id, key, value_json, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, $4, $5)
		ON CONFLICT(user_id, key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, strings.TrimSpace(input.UserID), strings.TrimSpace(input.Key), mustJSON(ensureMap(input.Value)), now, now)
	if err != nil {
		return common.UserConfig{}, err
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
		return common.ErrNotFound
	}
	return nil
}

func (s *Store) ListUserConfigs(ctx context.Context, userID string) ([]common.UserConfig, error) {
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

	items := make([]common.UserConfig, 0)
	for rows.Next() {
		item, err := scanUserConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetAuthVerificationCode(ctx context.Context, email string, purpose string) (common.AuthVerificationCode, error) {
	return scanAuthVerificationCode(s.pool.QueryRow(ctx, `
		SELECT email, purpose, code_hash, failed_attempts, expires_at, last_sent_at, created_at, updated_at
		FROM auth_verification_codes
		WHERE email = $1 AND purpose = $2
	`, strings.TrimSpace(strings.ToLower(email)), strings.TrimSpace(purpose)))
}

func (s *Store) UpsertAuthVerificationCode(ctx context.Context, input common.UpsertAuthVerificationCodeInput) (common.AuthVerificationCode, error) {
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
		return common.AuthVerificationCode{}, err
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
		return common.ErrNotFound
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
