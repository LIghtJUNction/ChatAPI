package service

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/store"
)

type AuditService struct {
	store store.Store
}

type AuditEventInput struct {
	EventType    string
	ResourceType string
	ResourceID   string
	Action       string
	Outcome      string
	IPAddress    string
	UserAgent    string
	Metadata     map[string]any
}

type ListAuditLogsInput struct {
	Limit         int
	EventType     string
	ActorUserID   string
	IncludeAppAPI bool
}

func NewAuditService(dataStore store.Store) *AuditService {
	return &AuditService{store: dataStore}
}

func (s *AuditService) Record(ctx context.Context, input AuditEventInput) {
	if s == nil || s.store == nil {
		return
	}
	eventType := strings.TrimSpace(input.EventType)
	action := strings.TrimSpace(input.Action)
	if eventType == "" || action == "" {
		return
	}
	outcome := strings.TrimSpace(input.Outcome)
	if outcome == "" {
		outcome = "success"
	}
	actor, _ := RequestActorFromContext(ctx)
	_, _ = s.store.CreateAuditLog(ctx, store.CreateAuditLogInput{
		ID:           "audit_" + uuid.NewString(),
		ActorUserID:  strings.TrimSpace(actor.UserID),
		ActorRole:    strings.TrimSpace(actor.Role),
		ActorSource:  strings.TrimSpace(actor.Source),
		EventType:    eventType,
		ResourceType: strings.TrimSpace(input.ResourceType),
		ResourceID:   strings.TrimSpace(input.ResourceID),
		Action:       action,
		Outcome:      outcome,
		IPAddress:    strings.TrimSpace(input.IPAddress),
		UserAgent:    strings.TrimSpace(input.UserAgent),
		Metadata:     sanitizeAuditMetadata(input.Metadata),
	})
}

func (s *AuditService) List(ctx context.Context, input ListAuditLogsInput) ([]store.AuditLog, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	items, err := s.store.ListAuditLogs(ctx, store.ListAuditLogsInput{
		Limit:       limit,
		EventType:   strings.TrimSpace(input.EventType),
		ActorUserID: strings.TrimSpace(input.ActorUserID),
	})
	if err != nil {
		return nil, err
	}
	if input.IncludeAppAPI && shouldIncludeAppAPIAudit(input.EventType) {
		appItems, err := s.store.ListAppAPIKeyAuditLogs(ctx, store.ListAppAPIKeyAuditLogsInput{
			Limit:  limit,
			UserID: strings.TrimSpace(input.ActorUserID),
		})
		if err != nil {
			return nil, err
		}
		for _, appItem := range appItems {
			items = append(items, appAPIKeyAuditLogToAuditLog(appItem))
		}
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].ID > items[j].ID
			}
			return items[i].CreatedAt.After(items[j].CreatedAt)
		})
		if len(items) > limit {
			items = items[:limit]
		}
	}
	return items, nil
}

func shouldIncludeAppAPIAudit(eventType string) bool {
	eventType = strings.TrimSpace(eventType)
	return eventType == "" || eventType == "app_api.request"
}

func appAPIKeyAuditLogToAuditLog(item store.AppAPIKeyAuditLog) store.AuditLog {
	outcome := "success"
	if item.StatusCode >= 400 {
		outcome = "failure"
	}
	metadata := map[string]any{
		"route":       item.Route,
		"status_code": item.StatusCode,
	}
	if strings.TrimSpace(item.ErrorCode) != "" {
		metadata["error_code"] = strings.TrimSpace(item.ErrorCode)
	}
	return store.AuditLog{
		ID:           item.ID,
		ActorUserID:  strings.TrimSpace(item.UserID),
		ActorRole:    "user",
		ActorSource:  "app_api_key",
		EventType:    "app_api.request",
		ResourceType: "app_api_key",
		ResourceID:   strings.TrimSpace(item.AppAPIKeyID),
		Action:       "request",
		Outcome:      outcome,
		Metadata:     metadata,
		CreatedAt:    item.CreatedAt,
	}
}

func sanitizeAuditMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	safe := make(map[string]any, len(metadata))
	for key, value := range metadata {
		normalized := strings.ToLower(strings.TrimSpace(key))
		switch {
		case normalized == "":
			continue
		case strings.Contains(normalized, "password"):
			continue
		case strings.Contains(normalized, "secret"):
			continue
		case strings.Contains(normalized, "token"):
			continue
		case strings.Contains(normalized, "authorization"):
			continue
		case strings.Contains(normalized, "key"):
			continue
		default:
			safe[key] = value
		}
	}
	if len(safe) == 0 {
		return nil
	}
	return safe
}
