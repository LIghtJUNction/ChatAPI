package postgresql

import (
	"context"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

func (s *Store) ListAutomationRulesByUser(ctx context.Context, userID string) ([]store.AutomationRule, error) {
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
	userID = strings.TrimSpace(userID)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if len(replaceIDs) == 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM automation_rules WHERE user_id = $1`, userID); err != nil {
			return nil, err
		}
	} else {
		for ruleID := range replaceIDs {
			if _, err := tx.Exec(ctx, `DELETE FROM automation_rules WHERE user_id = $1 AND id = $2`, userID, strings.TrimSpace(ruleID)); err != nil {
				return nil, err
			}
		}
	}

	now := time.Now().UTC()
	for _, input := range inputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO automation_rules(id, user_id, enabled, rule_json, created_at, updated_at)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6)
			ON CONFLICT(user_id, id) DO UPDATE SET
				enabled = excluded.enabled,
				rule_json = excluded.rule_json,
				updated_at = excluded.updated_at
		`,
			strings.TrimSpace(input.ID),
			userID,
			input.Enabled,
			mustJSON(ensureMap(input.Payload)),
			now,
			now,
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.ListAutomationRulesByUser(ctx, userID)
}

func scanAutomationRule(row rowScanner) (store.AutomationRule, error) {
	var item store.AutomationRule
	var payloadJSON []byte
	if err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.Enabled,
		&payloadJSON,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return store.AutomationRule{}, err
	}
	item.Payload = parseJSONMap(payloadJSON)
	return item, nil
}
