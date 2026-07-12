package postgresql

import (
	"context"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) ListAutomationRulesByUser(ctx context.Context, userID string) ([]common.AutomationRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, enabled, rule_json, created_at, updated_at
		FROM automation_rules
		WHERE user_id = $1
		ORDER BY updated_at DESC, id ASC
	`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.AutomationRule, 0)
	for rows.Next() {
		item, err := scanAutomationRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertAutomationRule(ctx context.Context, input common.UpsertAutomationRuleInput) (common.AutomationRule, error) {
	now := time.Now().UTC()
	row := s.pool.QueryRow(ctx, `
		INSERT INTO automation_rules(id, user_id, enabled, rule_json, created_at, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6)
		ON CONFLICT(user_id, id) DO UPDATE SET
			enabled = excluded.enabled,
			rule_json = excluded.rule_json,
			updated_at = excluded.updated_at
		RETURNING id, user_id, enabled, rule_json, created_at, updated_at
	`, strings.TrimSpace(input.ID), strings.TrimSpace(input.UserID), input.Enabled, mustJSON(ensureMap(input.Payload)), now, now)
	return scanAutomationRule(row)
}

func (s *Store) DeleteAutomationRule(ctx context.Context, userID string, ruleID string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM automation_rules WHERE user_id = $1 AND id = $2`, strings.TrimSpace(userID), strings.TrimSpace(ruleID))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return common.ErrNotFound
	}
	return nil
}

func scanAutomationRule(row rowScanner) (common.AutomationRule, error) {
	var item common.AutomationRule
	var payloadJSON []byte
	if err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.Enabled,
		&payloadJSON,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return common.AutomationRule{}, err
	}
	item.Payload = parseJSONMap(payloadJSON)
	return item, nil
}
