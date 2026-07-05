package store

import (
	"context"
	"errors"
	"time"
)

var ErrTurnConflict = errors.New("turn state conflict")
var ErrNotFound = errors.New("record not found")

type Conversation struct {
	ID                 string         `json:"id"`
	Title              string         `json:"title"`
	LastUserText       string         `json:"last_user_text"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	LastMessageAt      time.Time      `json:"last_message_at"`
	MessageCount       int            `json:"message_count"`
	LastMessagePreview string         `json:"last_message_preview"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	ResponseID         string         `json:"-"`
}

type Message struct {
	ID         string         `json:"id"`
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	CreatedAt  time.Time      `json:"created_at"`
	Status     string         `json:"status,omitempty"`
	ResponseID *string        `json:"response_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Request struct {
	RequestID      string         `json:"request_id"`
	OwnerID        string         `json:"owner_id,omitempty"`
	ConversationID string         `json:"conversation_id"`
	ResponseID     string         `json:"response_id,omitempty"`
	RequestFormat  string         `json:"request_format,omitempty"`
	Model          string         `json:"model,omitempty"`
	InputText      string         `json:"input_text,omitempty"`
	Status         string         `json:"status,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	RequestBody    map[string]any `json:"request_body,omitempty"`
	ToolSchemas    []any          `json:"tool_schemas,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type AppAPIKey struct {
	ID             string         `json:"id"`
	UserID         string         `json:"user_id"`
	Name           string         `json:"name"`
	KeyHash        string         `json:"-"`
	KeyPrefix      string         `json:"key_prefix"`
	Scopes         []string       `json:"scopes,omitempty"`
	ResourceLimits map[string]any `json:"resource_limits,omitempty"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
	LastUsedAt     *time.Time     `json:"last_used_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	RevokedAt      *time.Time     `json:"revoked_at,omitempty"`
}

type AppAPIKeyAuditLog struct {
	ID          string    `json:"id"`
	AppAPIKeyID string    `json:"app_api_key_id"`
	UserID      string    `json:"user_id"`
	Route       string    `json:"route"`
	StatusCode  int       `json:"status_code"`
	ErrorCode   string    `json:"error_code,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type ListAppAPIKeyAuditLogsInput struct {
	Limit  int
	UserID string
}

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

type ModelAPIKey struct {
	ID            string     `json:"id"`
	UserID        string     `json:"user_id"`
	Name          string     `json:"name"`
	KeyCiphertext string     `json:"-"`
	KeyPrefix     string     `json:"key_prefix"`
	Model         string     `json:"model,omitempty"`
	RawKey        string     `json:"raw_key,omitempty"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
}

type AutomationRule struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	Enabled   bool           `json:"enabled"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type UploadedImage struct {
	ID               string    `json:"id"`
	OwnerID          string    `json:"owner_id"`
	Filename         string    `json:"filename"`
	OriginalFilename string    `json:"original_filename,omitempty"`
	ContentType      string    `json:"content_type"`
	Bytes            int64     `json:"bytes"`
	URL              string    `json:"url"`
	CreatedAt        time.Time `json:"created_at"`
}

type StorageUserQuota struct {
	OwnerID    string    `json:"owner_id"`
	QuotaBytes int64     `json:"quota_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type MigrationStatus struct {
	SchemaVersion  string             `json:"schema_version"`
	AppVersion     string             `json:"app_version,omitempty"`
	MigrationDirty bool               `json:"migration_dirty"`
	MigrationLock  string             `json:"migration_lock,omitempty"`
	CreatedBy      string             `json:"created_by,omitempty"`
	LastMigratedAt string             `json:"last_migrated_at,omitempty"`
	Applied        []AppliedMigration `json:"applied"`
}

type AppliedMigration struct {
	Version   string `json:"version"`
	Name      string `json:"name,omitempty"`
	AppliedAt string `json:"applied_at"`
	Checksum  string `json:"checksum,omitempty"`
	Dirty     bool   `json:"dirty"`
}

type CreateAppAPIKeyInput struct {
	ID             string
	UserID         string
	Name           string
	KeyHash        string
	KeyPrefix      string
	Scopes         []string
	ResourceLimits map[string]any
	ExpiresAt      *time.Time
}

type CreateModelAPIKeyInput struct {
	ID            string
	UserID        string
	Name          string
	KeyCiphertext string
	KeyPrefix     string
	Model         string
}

type UpsertAutomationRuleInput struct {
	ID      string
	UserID  string
	Enabled bool
	Payload map[string]any
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

type CreateUploadedImageInput struct {
	ID               string
	OwnerID          string
	Filename         string
	OriginalFilename string
	ContentType      string
	Bytes            int64
	URL              string
}

type CreatePendingInput struct {
	ConversationID string
	RequestID      string
	ResponseID     string
	OwnerID        string
	RequestFormat  string
	Model          string
	UserContent    string
	RequestBody    map[string]any
	ToolSchemas    []any
}

type CompletePendingInput struct {
	ConversationID      string
	ResponseID          string
	OutputText          string
	Mode                string
	ToolName            string
	ToolCallID          string
	ToolOutput          string
	ReasoningStreamMode string
}

type UpdateDraftInput struct {
	ConversationID string
	DraftText      string
}

type AbortPendingInput struct {
	ConversationID string
	Reason         string
}

type DeleteConversationsResult struct {
	DeletedConversations int `json:"deleted_conversations"`
	DeletedMessages      int `json:"deleted_messages"`
}

type ExpirePendingTurnsResult struct {
	ExpiredConversations int `json:"expired_conversations"`
}

type Store interface {
	Ping(context.Context) error
	MigrationStatus(context.Context) (MigrationStatus, error)
	ListConversations(context.Context) ([]Conversation, error)
	GetConversation(context.Context, string) (Conversation, error)
	ListRequests(context.Context) ([]Request, error)
	GetRequest(context.Context, string) (Request, error)
	CreateAppAPIKey(context.Context, CreateAppAPIKeyInput) (AppAPIKey, error)
	ListAppAPIKeysByUser(context.Context, string) ([]AppAPIKey, error)
	GetAppAPIKeyByPrefix(context.Context, string) (AppAPIKey, error)
	UpdateAppAPIKeyLastUsedAt(context.Context, string, time.Time) error
	RevokeAppAPIKey(context.Context, string, string) error
	CreateAppAPIKeyAuditLog(context.Context, AppAPIKeyAuditLog) error
	ListAppAPIKeyAuditLogs(context.Context, ListAppAPIKeyAuditLogsInput) ([]AppAPIKeyAuditLog, error)
	CreateAuditLog(context.Context, CreateAuditLogInput) (AuditLog, error)
	ListAuditLogs(context.Context, ListAuditLogsInput) ([]AuditLog, error)
	CreateModelAPIKey(context.Context, CreateModelAPIKeyInput) (ModelAPIKey, error)
	ListModelAPIKeysByUser(context.Context, string) ([]ModelAPIKey, error)
	GetModelAPIKeyByPrefix(context.Context, string) (ModelAPIKey, error)
	GetModelAPIKeyByID(context.Context, string) (ModelAPIKey, error)
	UpdateModelAPIKeyLastUsedAt(context.Context, string, time.Time) error
	RevokeModelAPIKey(context.Context, string, string) error
	ListAutomationRulesByUser(context.Context, string) ([]AutomationRule, error)
	ReplaceAutomationRulesForUser(context.Context, string, map[string]struct{}, []UpsertAutomationRuleInput) ([]AutomationRule, error)
	CreateUploadedImage(context.Context, CreateUploadedImageInput) (UploadedImage, error)
	ListUploadedImages(context.Context) ([]UploadedImage, error)
	ListUploadedImagesByOwner(context.Context, string) ([]UploadedImage, error)
	ListStorageUserQuotas(context.Context) ([]StorageUserQuota, error)
	GetStorageUserQuota(context.Context, string) (StorageUserQuota, error)
	SetStorageUserQuota(context.Context, string, int64) (StorageUserQuota, error)
	DeleteStorageUserQuota(context.Context, string) error
	ListMessages(context.Context, string) ([]Message, error)
	DeleteConversations(context.Context, []string) (DeleteConversationsResult, error)
	ExpirePendingTurns(context.Context, time.Time) (ExpirePendingTurnsResult, error)
	CreatePendingTurn(context.Context, CreatePendingInput) (Conversation, Message, error)
	UpdateDraft(context.Context, UpdateDraftInput) (Conversation, error)
	CompletePendingTurn(context.Context, CompletePendingInput) (Conversation, Message, error)
	AbortPendingTurn(context.Context, AbortPendingInput) (Conversation, Message, error)
}
