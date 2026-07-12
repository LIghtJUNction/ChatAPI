package sqlite

import (
	"context"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) ListAutomationRulesByUser(ctx context.Context, userID string) ([]common.AutomationRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, enabled, rule_json, created_at, updated_at
		FROM automation_rules
		WHERE user_id = ?
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
	now := formatTime(time.Now().UTC())
	userID := strings.TrimSpace(input.UserID)
	ruleID := strings.TrimSpace(input.ID)
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO automation_rules(id, user_id, enabled, rule_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, id) DO UPDATE SET
			enabled = excluded.enabled,
			rule_json = excluded.rule_json,
			updated_at = excluded.updated_at
		RETURNING id, user_id, enabled, rule_json, created_at, updated_at
	`, ruleID, userID, boolInt(input.Enabled), mustJSON(ensureMap(input.Payload)), now, now)
	return scanAutomationRule(row)
}

func (s *Store) DeleteAutomationRule(ctx context.Context, userID string, ruleID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM automation_rules WHERE user_id = ? AND id = ?`, strings.TrimSpace(userID), strings.TrimSpace(ruleID))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return common.ErrNotFound
	}
	return nil
}
