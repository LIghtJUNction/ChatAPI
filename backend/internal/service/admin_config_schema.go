package service

type AdminConfigSchema struct {
	Operations []AdminConfigOperationSchema `json:"operations"`
}

type AdminConfigOperationSchema struct {
	Name            string              `json:"name"`
	Method          string              `json:"method"`
	Path            string              `json:"path"`
	Description     string              `json:"description"`
	Fields          []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresAdmin   bool                `json:"requires_admin"`
	AllowUnknownTop bool                `json:"allow_unknown_top_level_keys,omitempty"`
	Notes           []string            `json:"notes,omitempty"`
}

func BuildAdminConfigSchema() AdminConfigSchema {
	return AdminConfigSchema{
		Operations: []AdminConfigOperationSchema{
			{
				Name:          "get_config",
				Method:        "GET",
				Path:          "/api/admin/config",
				Description:   "List persisted system config objects and the aggregated config map.",
				RequiresAdmin: true,
				Notes: []string{
					"The response shape is {ok, items, config}.",
				},
			},
			{
				Name:            "set_config",
				Method:          "POST",
				Path:            "/api/admin/config",
				Description:     "Persist arbitrary system config objects keyed by top-level config name.",
				RequiresAdmin:   true,
				AllowUnknownTop: true,
				Fields: []ConfigFieldSchema{
					{Key: "config", ValueType: "object", DefaultValue: map[string]any{}, Public: false, AdminWriteOnly: true, Description: "Optional envelope. When provided, its nested object is used as the actual config payload.", Validation: map[string]any{"additional_properties": map[string]any{"type": "object"}}},
				},
				Notes: []string{
					"This endpoint accepts either a raw top-level object map or {config:{...}}.",
					"Each top-level value must itself be a JSON object; scalar and array values are rejected.",
					"Use /api/config/system/schema for the productized system settings surface; /api/admin/config remains the low-level free-form store.",
				},
			},
		},
	}
}
