package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) GetSystemConfig(ctx context.Context, key string) (common.SystemConfig, error) {
	item, err := scanSystemConfig(s.db.QueryRowContext(ctx, `
		SELECT key, value_json, created_at, updated_at
		FROM config
		WHERE key = ?
	`, strings.TrimSpace(key)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.SystemConfig{}, errNotFound
		}
		return common.SystemConfig{}, err
	}
	return item, nil
}

func (s *Store) SetSystemConfig(ctx context.Context, input common.SetSystemConfigInput) (common.SystemConfig, error) {
	now := time.Now().UTC()
	key := strings.TrimSpace(input.Key)
	valueJSON := mustJSON(ensureMap(input.Value))
	// System settings intentionally use last-write-wins. This project does not
	// coordinate concurrent administrator edits or reject a later submission.
	return scanSystemConfig(s.db.QueryRowContext(ctx, `
		INSERT INTO config(key,value_json,created_at,updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET
			value_json=excluded.value_json,
			updated_at=excluded.updated_at
		RETURNING key,value_json,created_at,updated_at
	`, key, valueJSON, formatTime(now), formatTime(now)))
}

func (s *Store) DeleteSystemConfig(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM config WHERE key = ?`, strings.TrimSpace(key))
	return err
}

func (s *Store) ListSystemConfigs(ctx context.Context) ([]common.SystemConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
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
	item, err := scanUserConfig(s.db.QueryRowContext(ctx, `
		SELECT user_id, key, value_json, created_at, updated_at
		FROM user_configs
		WHERE user_id = ? AND key = ?
	`, strings.TrimSpace(userID), strings.TrimSpace(key)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.UserConfig{}, errNotFound
		}
		return common.UserConfig{}, err
	}
	return item, nil
}

func (s *Store) SetUserConfig(ctx context.Context, input common.SetUserConfigInput) (common.UserConfig, error) {
	now := time.Now().UTC()
	userID := strings.TrimSpace(input.UserID)
	key := strings.TrimSpace(input.Key)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO user_configs(user_id, key, value_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, userID, key, mustJSON(ensureMap(input.Value)), formatTime(now), formatTime(now)); err != nil {
		return common.UserConfig{}, err
	}
	return s.GetUserConfig(ctx, userID, key)
}

func (s *Store) DeleteUserConfig(ctx context.Context, userID string, key string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM user_configs
		WHERE user_id = ? AND key = ?
	`, strings.TrimSpace(userID), strings.TrimSpace(key))
	return err
}

func (s *Store) ListUserConfigs(ctx context.Context, userID string) ([]common.UserConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, key, value_json, created_at, updated_at
		FROM user_configs
		WHERE user_id = ?
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
	item, err := scanAuthVerificationCode(s.db.QueryRowContext(ctx, `
		SELECT email, purpose, code_hash, failed_attempts, expires_at, last_sent_at, created_at, updated_at
		FROM auth_verification_codes
		WHERE email = ? AND purpose = ?
	`, strings.TrimSpace(email), strings.TrimSpace(purpose)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.AuthVerificationCode{}, errNotFound
		}
		return common.AuthVerificationCode{}, err
	}
	return item, nil
}

func (s *Store) UpsertAuthVerificationCode(ctx context.Context, input common.UpsertAuthVerificationCodeInput) (common.AuthVerificationCode, error) {
	now := time.Now().UTC()
	email := strings.TrimSpace(strings.ToLower(input.Email))
	purpose := strings.TrimSpace(input.Purpose)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_verification_codes(email, purpose, code_hash, failed_attempts, expires_at, last_sent_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(email, purpose) DO UPDATE SET
			code_hash = excluded.code_hash,
			failed_attempts = excluded.failed_attempts,
			expires_at = excluded.expires_at,
			last_sent_at = excluded.last_sent_at,
			updated_at = excluded.updated_at
	`, email, purpose, strings.TrimSpace(input.CodeHash), input.FailedAttempts, formatTime(input.ExpiresAt.UTC()), formatTime(input.LastSentAt.UTC()), formatTime(now), formatTime(now)); err != nil {
		return common.AuthVerificationCode{}, err
	}
	return s.GetAuthVerificationCode(ctx, email, purpose)
}

func (s *Store) DeleteAuthVerificationCode(ctx context.Context, email string, purpose string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM auth_verification_codes
		WHERE email = ? AND purpose = ?
	`, strings.TrimSpace(strings.ToLower(email)), strings.TrimSpace(purpose))
	return err
}

func (s *Store) DeleteExpiredAuthVerificationCodes(ctx context.Context, before time.Time) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM auth_verification_codes
		WHERE expires_at <= ?
	`, formatTime(before.UTC()))
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(rows), nil
}
