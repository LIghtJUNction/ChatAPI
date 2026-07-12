package postgresql

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) countInt(ctx context.Context, query string, args ...any) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func scanUser(row rowScanner) (common.User, error) {
	var item common.User
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
			return common.User{}, common.ErrNotFound
		}
		return common.User{}, err
	}
	item.LastLoginAt = lastLoginAt
	return item, nil
}

func scanUserIdentity(row rowScanner) (common.UserIdentity, error) {
	var item common.UserIdentity
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
			return common.UserIdentity{}, common.ErrNotFound
		}
		return common.UserIdentity{}, err
	}
	item.Profile = parseJSONMap(profileJSON)
	item.LastLoginAt = lastLoginAt
	return item, nil
}

func scanSystemConfig(row rowScanner) (common.SystemConfig, error) {
	var item common.SystemConfig
	var valueJSON []byte
	if err := row.Scan(&item.Key, &valueJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.SystemConfig{}, common.ErrNotFound
		}
		return common.SystemConfig{}, err
	}
	item.Value = parseJSONMap(valueJSON)
	return item, nil
}

func scanAuthVerificationCode(row rowScanner) (common.AuthVerificationCode, error) {
	var item common.AuthVerificationCode
	if err := row.Scan(&item.Email, &item.Purpose, &item.CodeHash, &item.FailedAttempts, &item.ExpiresAt, &item.LastSentAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.AuthVerificationCode{}, common.ErrNotFound
		}
		return common.AuthVerificationCode{}, err
	}
	return item, nil
}

func scanUserConfig(row rowScanner) (common.UserConfig, error) {
	var item common.UserConfig
	var valueJSON []byte
	if err := row.Scan(&item.UserID, &item.Key, &valueJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.UserConfig{}, common.ErrNotFound
		}
		return common.UserConfig{}, err
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

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
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
