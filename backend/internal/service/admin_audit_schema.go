package service

type AdminAuditSchema struct {
	Operations []AdminAuditOperationSchema `json:"operations"`
}

type AdminAuditOperationSchema struct {
	Name          string              `json:"name"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	Description   string              `json:"description"`
	Fields        []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresAdmin bool                `json:"requires_admin"`
	Notes         []string            `json:"notes,omitempty"`
}

func BuildAdminAuditSchema() AdminAuditSchema {
	return AdminAuditSchema{
		Operations: []AdminAuditOperationSchema{
			{
				Name:          "list_audit_logs",
				Method:        "GET",
				Path:          "/api/admin/audit/logs",
				Description:   "List audit logs with optional filters and optional app API audit projection.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "limit", ValueType: "integer", DefaultValue: 50, Public: false, AdminWriteOnly: true, Description: "Maximum number of audit records to return.", Validation: map[string]any{"min": 1, "max": 200}},
					{Key: "event_type", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional event type filter such as upload, admin.runtime, admin.storage, auth.session or app_api.request."},
					{Key: "actor_user_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional actor user id filter."},
					{Key: "include_app_api", ValueType: "boolean", DefaultValue: false, Public: false, AdminWriteOnly: true, Description: "When true and the event filter allows it, merge app_api_key_audit_logs into the unified response shape."},
				},
				Notes: []string{
					"Without include_app_api, the response only includes rows from audit_logs.",
					"include_app_api only affects event_type=app_api.request or an empty event_type filter.",
					"The response shape is {ok, count, items}. count is the number of returned rows after merge and truncation.",
				},
			},
		},
	}
}
