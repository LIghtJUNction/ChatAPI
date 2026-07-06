package postgresql

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zyf/chatapi/internal/store"
)

func (s *Store) CreateAuditLog(ctx context.Context, input store.CreateAuditLogInput) (store.AuditLog, error) {
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
		return store.AuditLog{}, err
	}
	return store.AuditLog{
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

func (s *Store) ListAuditLogs(ctx context.Context, input store.ListAuditLogsInput) ([]store.AuditLog, error) {
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
		return nil, err
	}
	defer rows.Close()

	items := make([]store.AuditLog, 0)
	for rows.Next() {
		item, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanAuditLog(row rowScanner) (store.AuditLog, error) {
	var item store.AuditLog
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
			return store.AuditLog{}, store.ErrNotFound
		}
		return store.AuditLog{}, err
	}
	item.Metadata = parseJSONMap(metadataJSON)
	return item, nil
}
