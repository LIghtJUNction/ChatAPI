package audit

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zyf/chatapi/internal/actor"
	auditrepo "github.com/zyf/chatapi/internal/repository/audit"
	"github.com/zyf/chatapi/internal/repository/common"
)

type Service struct {
	store auditrepo.Store
}

type ListInput struct {
	Limit         int
	EventType     string
	ActorUserID   string
	IncludeAppAPI bool
}

func NewService(dataStore auditrepo.Store) *Service {
	return &Service{store: dataStore}
}

func (s *Service) Record(ctx context.Context, input common.CreateAuditLogInput) (common.AuditLog, error) {
	if strings.TrimSpace(input.ID) == "" {
		input.ID = "audit_" + uuid.NewString()
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	return s.store.CreateAuditLog(ctx, input)
}

func (s *Service) RecordActor(ctx context.Context, act actor.Actor, eventType string, resourceType string, resourceID string, action string, outcome string, metadata map[string]any) (common.AuditLog, error) {
	return s.Record(ctx, common.CreateAuditLogInput{
		ActorUserID:  strings.TrimSpace(act.UserID),
		ActorRole:    strings.TrimSpace(act.Role),
		ActorSource:  strings.TrimSpace(act.Source),
		EventType:    strings.TrimSpace(eventType),
		ResourceType: strings.TrimSpace(resourceType),
		ResourceID:   strings.TrimSpace(resourceID),
		Action:       strings.TrimSpace(action),
		Outcome:      strings.TrimSpace(outcome),
		Metadata:     metadata,
	})
}

func (s *Service) List(ctx context.Context, input ListInput) ([]map[string]any, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	items, err := s.store.ListAuditLogs(ctx, common.ListAuditLogsInput{
		Limit:       limit,
		EventType:   strings.TrimSpace(input.EventType),
		ActorUserID: strings.TrimSpace(input.ActorUserID),
	})
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, mapAuditLog(item))
	}
	if input.IncludeAppAPI {
		appLogs, err := s.store.ListAppAPIKeyAuditLogs(ctx, common.ListAppAPIKeyAuditLogsInput{
			Limit:  limit,
			UserID: strings.TrimSpace(input.ActorUserID),
		})
		if err != nil {
			return nil, err
		}
		for _, item := range appLogs {
			if input.EventType != "" && !strings.EqualFold(strings.TrimSpace(input.EventType), "app_api.request") {
				continue
			}
			result = append(result, mapAppAuditLog(item))
		}
		sort.Slice(result, func(i, j int) bool {
			return createdAtOf(result[i]).After(createdAtOf(result[j]))
		})
		if len(result) > limit {
			result = result[:limit]
		}
	}
	return result, nil
}

func mapAuditLog(item common.AuditLog) map[string]any {
	return map[string]any{
		"id":            item.ID,
		"type":          "audit_log",
		"event_type":    item.EventType,
		"action":        item.Action,
		"outcome":       item.Outcome,
		"actor_user_id": item.ActorUserID,
		"actor_role":    item.ActorRole,
		"actor_source":  item.ActorSource,
		"resource_type": item.ResourceType,
		"resource_id":   item.ResourceID,
		"ip_address":    item.IPAddress,
		"user_agent":    item.UserAgent,
		"metadata":      item.Metadata,
		"created_at":    item.CreatedAt,
	}
}

func mapAppAuditLog(item common.AppAPIKeyAuditLog) map[string]any {
	return map[string]any{
		"id":             item.ID,
		"type":           "app_api_audit_log",
		"event_type":     "app_api.request",
		"action":         "request",
		"outcome":        appAuditOutcome(item.StatusCode),
		"actor_user_id":  item.UserID,
		"actor_role":     "app_api",
		"actor_source":   "app_api_key",
		"resource_type":  "app_api_key",
		"resource_id":    item.AppAPIKeyID,
		"route":          item.Route,
		"http_status":    item.StatusCode,
		"error_code":     item.ErrorCode,
		"app_api_key_id": item.AppAPIKeyID,
		"created_at":     item.CreatedAt,
	}
}

func appAuditOutcome(statusCode int) string {
	if statusCode >= 200 && statusCode < 400 {
		return "success"
	}
	return "failure"
}

func createdAtOf(item map[string]any) time.Time {
	value, _ := item["created_at"].(time.Time)
	return value
}
