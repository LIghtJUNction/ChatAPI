package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

func (s *Store) GetSystemConfig(ctx context.Context, key string) (store.SystemConfig, error) {
	item, err := scanSystemConfig(s.db.QueryRowContext(ctx, `
		SELECT key, value_json, created_at, updated_at
		FROM config
		WHERE key = ?
	`, strings.TrimSpace(key)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.SystemConfig{}, errNotFound
		}
		return store.SystemConfig{}, err
	}
	return item, nil
}

func (s *Store) SetSystemConfig(ctx context.Context, input store.SetSystemConfigInput) (store.SystemConfig, error) {
	now := time.Now().UTC()
	key := strings.TrimSpace(input.Key)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO config(key, value_json, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, key, mustJSON(ensureMap(input.Value)), formatTime(now), formatTime(now)); err != nil {
		return store.SystemConfig{}, err
	}
	return s.GetSystemConfig(ctx, key)
}

func (s *Store) DeleteSystemConfig(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM config WHERE key = ?`, strings.TrimSpace(key))
	return err
}

func (s *Store) ListSystemConfigs(ctx context.Context) ([]store.SystemConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
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
	item, err := scanUserConfig(s.db.QueryRowContext(ctx, `
		SELECT user_id, key, value_json, created_at, updated_at
		FROM user_configs
		WHERE user_id = ? AND key = ?
	`, strings.TrimSpace(userID), strings.TrimSpace(key)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.UserConfig{}, errNotFound
		}
		return store.UserConfig{}, err
	}
	return item, nil
}

func (s *Store) SetUserConfig(ctx context.Context, input store.SetUserConfigInput) (store.UserConfig, error) {
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
		return store.UserConfig{}, err
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

func (s *Store) ListUserConfigs(ctx context.Context, userID string) ([]store.UserConfig, error) {
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
	item, err := scanAuthVerificationCode(s.db.QueryRowContext(ctx, `
		SELECT email, purpose, code_hash, failed_attempts, expires_at, last_sent_at, created_at, updated_at
		FROM auth_verification_codes
		WHERE email = ? AND purpose = ?
	`, strings.TrimSpace(email), strings.TrimSpace(purpose)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.AuthVerificationCode{}, errNotFound
		}
		return store.AuthVerificationCode{}, err
	}
	return item, nil
}

func (s *Store) UpsertAuthVerificationCode(ctx context.Context, input store.UpsertAuthVerificationCodeInput) (store.AuthVerificationCode, error) {
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
		return store.AuthVerificationCode{}, err
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

func (s *Store) ListAutomationRulesByUser(ctx context.Context, userID string) ([]store.AutomationRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, enabled, rule_json, created_at, updated_at
		FROM automation_rules
		WHERE user_id = ?
		ORDER BY updated_at DESC, id ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.AutomationRule, 0)
	for rows.Next() {
		item, err := scanAutomationRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ReplaceAutomationRulesForUser(ctx context.Context, userID string, replaceIDs map[string]struct{}, inputs []store.UpsertAutomationRuleInput) ([]store.AutomationRule, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if len(replaceIDs) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM automation_rules WHERE user_id = ?`, userID); err != nil {
			return nil, err
		}
	} else {
		for ruleID := range replaceIDs {
			if _, err := tx.ExecContext(ctx, `DELETE FROM automation_rules WHERE user_id = ? AND id = ?`, userID, ruleID); err != nil {
				return nil, err
			}
		}
	}

	now := formatTime(time.Now().UTC())
	for _, input := range inputs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO automation_rules(id, user_id, enabled, rule_json, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(user_id, id) DO UPDATE SET
				enabled = excluded.enabled,
				rule_json = excluded.rule_json,
				updated_at = excluded.updated_at
		`,
			input.ID,
			userID,
			boolInt(input.Enabled),
			mustJSON(ensureMap(input.Payload)),
			now,
			now,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListAutomationRulesByUser(ctx, userID)
}
