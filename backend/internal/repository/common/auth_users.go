package common

import "time"

type ModelAPIKey struct {
	ID            string `json:"id"`
	UserID        string `json:"user_id"`
	Name          string `json:"name"`
	KeyCiphertext string `json:"-"`
	KeyPrefix     string `json:"key_prefix"`
	// Model is retained for reading pre-migration rows; virtual models are stored separately.
	Model      string     `json:"-"`
	RawKey     string     `json:"raw_key,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type VirtualModel struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID               string     `json:"id"`
	Username         string     `json:"username,omitempty"`
	Email            string     `json:"email,omitempty"`
	PasswordHash     string     `json:"-"`
	Role             string     `json:"role"`
	IsActive         bool       `json:"is_active"`
	LocalAdmin       bool       `json:"local_admin"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	LastLoginAt      *time.Time `json:"last_login_at,omitempty"`
	AppAPIKeyCount   int        `json:"app_api_key_count"`
	ModelAPIKeyCount int        `json:"model_api_key_count"`
}

type UserDeletionPreview struct {
	User        User                      `json:"user"`
	CanDelete   bool                      `json:"can_delete"`
	Blockers    []string                  `json:"blockers,omitempty"`
	Counts      UserDeletionPreviewCounts `json:"counts"`
	PreserveRef UserDeletionPreserveRef   `json:"preserve_refs"`
}

type UserOwnershipTransferResult struct {
	SourceUserID                 string `json:"source_user_id"`
	TargetUserID                 string `json:"target_user_id"`
	TransferredConversations     int    `json:"transferred_conversations"`
	TransferredUploadedImages    int    `json:"transferred_uploaded_images"`
	TransferredDeletionFailures  int    `json:"transferred_deletion_failures"`
	SourceQuotaDeleted           bool   `json:"source_quota_deleted"`
	TargetQuotaPreserved         bool   `json:"target_quota_preserved"`
	TargetQuotaCreatedFromSource bool   `json:"target_quota_created_from_source"`
}

type UserOwnedConversationItem struct {
	ConversationID string    `json:"conversation_id"`
	Title          string    `json:"title"`
	LastMessageAt  time.Time `json:"last_message_at"`
	MessageCount   int       `json:"message_count"`
}

type UserOwnedUploadItem struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	Bytes     int64     `json:"bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type UserOwnershipSelection struct {
	User          User                        `json:"user"`
	Conversations []UserOwnedConversationItem `json:"conversations"`
	Uploads       []UserOwnedUploadItem       `json:"uploads"`
}

type UserDeletionPreviewCounts struct {
	Identities                  int `json:"identities"`
	UserConfigs                 int `json:"user_configs"`
	AutomationRules             int `json:"automation_rules"`
	AppAPIKeys                  int `json:"app_api_keys"`
	AppAPIKeyAuditLogs          int `json:"app_api_key_audit_logs"`
	ModelAPIKeys                int `json:"model_api_keys"`
	StorageUserQuotas           int `json:"storage_user_quotas"`
	StorageDeletionFailures     int `json:"storage_deletion_failures"`
	OwnedConversations          int `json:"owned_conversations"`
	OwnedUploadedImages         int `json:"owned_uploaded_images"`
	AuditActorLogs              int `json:"audit_actor_logs"`
	AuditMetadataUserReferences int `json:"audit_metadata_user_references"`
}

type UserDeletionPreserveRef struct {
	AuditLogs     bool `json:"audit_logs"`
	Conversations bool `json:"conversations"`
	Uploads       bool `json:"uploads"`
}

type UserIdentity struct {
	ID            string         `json:"id"`
	UserID        string         `json:"user_id"`
	Provider      string         `json:"provider"`
	Subject       string         `json:"subject"`
	Email         string         `json:"email,omitempty"`
	EmailVerified bool           `json:"email_verified"`
	Profile       map[string]any `json:"profile,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	LastLoginAt   *time.Time     `json:"last_login_at,omitempty"`
}
