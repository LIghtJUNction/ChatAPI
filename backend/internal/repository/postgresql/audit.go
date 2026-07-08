package postgresql

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/zyf/chatapi/internal/repository/common"
)

func (s *Store) CreateAuditLog(ctx context.Context, input common.CreateAuditLogInput) (common.AuditLog, error) {
	createdAt := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_logs(
			id, actor_user_id, actor_role, actor_source, event_type, resource_type,
			resource_id, action, outcome, ip_address, user_agent, metadata_json, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13)
	`,
		input.ID,
		input.ActorUserID,
		input.ActorRole,
		input.ActorSource,
		input.EventType,
		input.ResourceType,
		input.ResourceID,
		input.Action,
		input.Outcome,
		input.IPAddress,
		input.UserAgent,
		mustJSON(ensureMap(input.Metadata)),
		createdAt,
	)
	if err != nil {
		s.logger(ctx).Warn("postgresql create audit log failed", zap.String("audit.event_type", input.EventType), zap.String("audit.action", input.Action), zap.Error(err))
		return common.AuditLog{}, err
	}
	return common.AuditLog{
		ID:           input.ID,
		ActorUserID:  input.ActorUserID,
		ActorRole:    input.ActorRole,
		ActorSource:  input.ActorSource,
		EventType:    input.EventType,
		ResourceType: input.ResourceType,
		ResourceID:   input.ResourceID,
		Action:       input.Action,
		Outcome:      input.Outcome,
		IPAddress:    input.IPAddress,
		UserAgent:    input.UserAgent,
		Metadata:     ensureMap(input.Metadata),
		CreatedAt:    createdAt,
	}, nil
}

func (s *Store) ListAuditLogs(ctx context.Context, input common.ListAuditLogsInput) ([]common.AuditLog, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	args := []any{limit}
	conditions := make([]string, 0, 2)
	if strings.TrimSpace(input.EventType) != "" {
		args = append(args, strings.TrimSpace(input.EventType))
		conditions = append(conditions, "event_type = $"+strconv.Itoa(len(args)))
	}
	if strings.TrimSpace(input.ActorUserID) != "" {
		args = append(args, strings.TrimSpace(input.ActorUserID))
		conditions = append(conditions, "actor_user_id = $"+strconv.Itoa(len(args)))
	}
	query := `
		SELECT id, actor_user_id, actor_role, actor_source, event_type, resource_type,
			resource_id, action, outcome, ip_address, user_agent, metadata_json, created_at
		FROM audit_logs
	`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT $1"
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		s.logger(ctx).Warn("postgresql list audit logs failed", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]common.AuditLog, 0)
	for rows.Next() {
		item, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("postgresql list audit logs row iteration failed", zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) CountAuditLogs(ctx context.Context, input common.CountAuditLogsInput) (int, error) {
	args := make([]any, 0, 5)
	conditions := make([]string, 0, 5)
	if strings.TrimSpace(input.EventType) != "" {
		args = append(args, strings.TrimSpace(input.EventType))
		conditions = append(conditions, "event_type = $"+strconv.Itoa(len(args)))
	}
	if strings.TrimSpace(input.ActorUserID) != "" {
		args = append(args, strings.TrimSpace(input.ActorUserID))
		conditions = append(conditions, "actor_user_id = $"+strconv.Itoa(len(args)))
	}
	if strings.TrimSpace(input.ResourceType) != "" {
		args = append(args, strings.TrimSpace(input.ResourceType))
		conditions = append(conditions, "resource_type = $"+strconv.Itoa(len(args)))
	}
	if strings.TrimSpace(input.Action) != "" {
		args = append(args, strings.TrimSpace(input.Action))
		conditions = append(conditions, "action = $"+strconv.Itoa(len(args)))
	}
	if strings.TrimSpace(input.Outcome) != "" {
		args = append(args, strings.TrimSpace(input.Outcome))
		conditions = append(conditions, "outcome = $"+strconv.Itoa(len(args)))
	}
	query := `SELECT COUNT(*) FROM audit_logs`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	var count int
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		s.logger(ctx).Warn("postgresql count audit logs failed", zap.Error(err))
		return 0, err
	}
	return count, nil
}

func scanAuditLog(row rowScanner) (common.AuditLog, error) {
	var item common.AuditLog
	var metadataJSON []byte
	if err := row.Scan(
		&item.ID,
		&item.ActorUserID,
		&item.ActorRole,
		&item.ActorSource,
		&item.EventType,
		&item.ResourceType,
		&item.ResourceID,
		&item.Action,
		&item.Outcome,
		&item.IPAddress,
		&item.UserAgent,
		&metadataJSON,
		&item.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.AuditLog{}, common.ErrNotFound
		}
		return common.AuditLog{}, err
	}
	item.Metadata = parseJSONMap(metadataJSON)
	return item, nil
}
