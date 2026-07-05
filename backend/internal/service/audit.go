package service

import (
	"context"
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
	Limit       int
	EventType   string
	ActorUserID string
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
	return s.store.ListAuditLogs(ctx, store.ListAuditLogsInput{
		Limit:       limit,
		EventType:   strings.TrimSpace(input.EventType),
		ActorUserID: strings.TrimSpace(input.ActorUserID),
	})
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
