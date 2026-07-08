package store

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
