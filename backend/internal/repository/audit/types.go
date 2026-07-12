package audit

import "time"

type AuditLog struct {
	ID           string         `json:"id"`
	ActorUserID  string         `json:"actor_user_id,omitempty"`
	ActorRole    string         `json:"actor_role,omitempty"`
	ActorSource  string         `json:"actor_source,omitempty"`
	EventType    string         `json:"event_type"`
	ResourceType string         `json:"resource_type,omitempty"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Action       string         `json:"action"`
	Outcome      string         `json:"outcome"`
	IPAddress    string         `json:"ip_address,omitempty"`
	UserAgent    string         `json:"user_agent,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type CreateAuditLogInput struct {
	ID           string
	ActorUserID  string
	ActorRole    string
	ActorSource  string
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

type CountAuditLogsInput struct {
	EventType    string
	ActorUserID  string
	ResourceType string
	Action       string
	Outcome      string
}
