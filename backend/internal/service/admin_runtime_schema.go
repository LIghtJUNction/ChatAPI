package service

type AdminRuntimeSchema struct {
	Operations []AdminRuntimeOperationSchema `json:"operations"`
}

type AdminRuntimeOperationSchema struct {
	Name          string              `json:"name"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	Description   string              `json:"description"`
	Fields        []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresAdmin bool                `json:"requires_admin"`
	Notes         []string            `json:"notes,omitempty"`
}

func BuildAdminRuntimeSchema() AdminRuntimeSchema {
	return AdminRuntimeSchema{
		Operations: []AdminRuntimeOperationSchema{
			{
				Name:          "automation_diagnostics",
				Method:        "GET",
				Path:          "/api/admin/runtime/automation",
				Description:   "Read automation diagnostics with optional filters.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "limit", ValueType: "integer", DefaultValue: 0, Public: false, AdminWriteOnly: true, Description: "Optional max number of recent skip samples to return. Zero means no explicit limit.", Validation: map[string]any{"min": 0}},
					{Key: "reason", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional skip reason filter."},
					{Key: "rule_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional automation rule id filter."},
				},
			},
			{
				Name:          "update_runtime_settings",
				Method:        "PUT",
				Path:          "/api/admin/runtime/settings",
				Description:   "Apply process runtime tuning values.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "gogc", ValueType: "integer", DefaultValue: defaultRuntimeGOGC, Public: false, AdminWriteOnly: true, Description: "Go GC percentage. Use 0 to reset to the Go default.", Validation: map[string]any{"min": 0}},
					{Key: "memory_limit_bytes", ValueType: "integer", DefaultValue: int64(0), Public: false, AdminWriteOnly: true, Description: "Soft Go memory limit in bytes. Use 0 to remove the explicit limit.", Validation: map[string]any{"min": 0}},
				},
				Notes: []string{
					"At least one field should be provided when updating runtime settings.",
					"Values are applied to the current process only.",
				},
			},
			{
				Name:          "force_gc",
				Method:        "POST",
				Path:          "/api/admin/runtime/gc",
				Description:   "Force a Go garbage collection cycle and return the post-GC memory snapshot.",
				RequiresAdmin: true,
				Notes: []string{
					"This endpoint does not accept a request body.",
				},
			},
		},
	}
}
