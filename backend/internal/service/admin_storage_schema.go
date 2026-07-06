package service

import "github.com/zyf/chatapi/internal/config"

type AdminStorageSchema struct {
	Operations []AdminStorageOperationSchema `json:"operations"`
}

type AdminStorageOperationSchema struct {
	Name          string              `json:"name"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	Description   string              `json:"description"`
	Fields        []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresAdmin bool                `json:"requires_admin"`
	Notes         []string            `json:"notes,omitempty"`
}

func BuildAdminStorageSchema(cfg config.Config) AdminStorageSchema {
	vacuumNotes := []string{
		"dry_run must always be provided explicitly.",
	}
	if cfg.DatabaseDriver == "sqlite" {
		vacuumNotes = append(vacuumNotes, "dry_run=false performs WAL checkpoint and VACUUM for sqlite deployments.")
	} else {
		vacuumNotes = append(vacuumNotes, "dry_run=false is not supported for non-sqlite deployments and will be rejected.")
	}
	return AdminStorageSchema{
		Operations: []AdminStorageOperationSchema{
			{
				Name:          "set_user_quota",
				Method:        "PUT",
				Path:          "/api/admin/storage/users/{owner_id}/quota",
				Description:   "Set or override a user's storage quota in bytes.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "quota_bytes", ValueType: "integer", DefaultValue: 0, Public: false, AdminWriteOnly: true, Description: "Quota override in bytes. Zero means no limit only if the deployment policy treats zero as unlimited.", Validation: map[string]any{"min": 0}},
				},
			},
			{
				Name:          "delete_user_quota",
				Method:        "DELETE",
				Path:          "/api/admin/storage/users/{owner_id}/quota",
				Description:   "Delete a user's quota override and fall back to the default quota.",
				RequiresAdmin: true,
			},
			{
				Name:          "cleanup_preview_or_execute",
				Method:        "POST",
				Path:          "/api/admin/storage/cleanup",
				Description:   "Preview or execute old-conversation cleanup and orphaned image reclamation.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "dry_run", ValueType: "boolean", DefaultValue: true, Public: false, AdminWriteOnly: true, Description: "Must be provided explicitly. true previews, false executes deletion."},
					{Key: "owner_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional owner filter to limit cleanup to one user."},
					{Key: "keep_recent_conversations", ValueType: "integer", DefaultValue: cfg.StorageCleanupKeepRecentConversations, Public: false, AdminWriteOnly: true, Description: "Number of recent conversations to preserve per owner before older closed conversations become candidates.", Validation: map[string]any{"min": 0}},
					{Key: "keep_recent_days", ValueType: "integer", DefaultValue: cfg.StorageCleanupKeepRecentDays, Public: false, AdminWriteOnly: true, Description: "Age window in days to preserve recent conversations.", Validation: map[string]any{"min": 0}},
				},
				Notes: []string{
					"waiting and streaming conversations are excluded from deletion.",
					"dry_run=false uses the same candidate algorithm as the preview response.",
				},
			},
			{
				Name:          "cleanup_orphans",
				Method:        "POST",
				Path:          "/api/admin/storage/orphans/cleanup",
				Description:   "Delete filesystem images that are no longer tracked in uploaded_images metadata.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "dry_run", ValueType: "boolean", DefaultValue: false, Public: false, AdminWriteOnly: true, Description: "Must be explicitly false. The preview endpoint is GET /api/admin/storage/orphans."},
				},
				Notes: []string{
					"dry_run=true is rejected; preview through GET /api/admin/storage/orphans instead.",
				},
			},
			{
				Name:          "vacuum",
				Method:        "POST",
				Path:          "/api/admin/storage/vacuum",
				Description:   "Preview or execute storage vacuum / checkpoint operations.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "dry_run", ValueType: "boolean", DefaultValue: true, Public: false, AdminWriteOnly: true, Description: "Must be provided explicitly for preview or execution."},
				},
				Notes: vacuumNotes,
			},
		},
	}
}
