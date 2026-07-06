package service

type AdminUsersSchema struct {
	Operations []AdminUsersOperationSchema `json:"operations"`
}

type AdminUsersOperationSchema struct {
	Name          string              `json:"name"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	Description   string              `json:"description"`
	Fields        []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresAdmin bool                `json:"requires_admin"`
	Notes         []string            `json:"notes,omitempty"`
}

func BuildAdminUsersSchema() AdminUsersSchema {
	return AdminUsersSchema{
		Operations: []AdminUsersOperationSchema{
			{
				Name:          "list_users",
				Method:        "GET",
				Path:          "/api/admin/users",
				Description:   "List local users available to the ChatAPI admin session.",
				RequiresAdmin: true,
				Notes: []string{
					"The response includes both items and users as aliases for the same user list.",
				},
			},
			{
				Name:          "create_user",
				Method:        "POST",
				Path:          "/api/admin/users",
				Description:   "Create a local user with an Argon2id password hash.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "username", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Unique local username."},
					{Key: "email", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Unique email address."},
					{Key: "password", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Initial password for the local user."},
					{Key: "role", ValueType: "string", DefaultValue: "user", Public: false, AdminWriteOnly: true, Description: "Role assigned to the user.", Validation: map[string]any{"enum": []string{"user", "admin"}}},
				},
			},
			{
				Name:          "list_user_history",
				Method:        "GET",
				Path:          "/api/admin/users/{user_id}/history",
				Description:   "Read a user's recent messages aggregated from their conversations.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "limit", ValueType: "integer", DefaultValue: 30, Public: false, AdminWriteOnly: true, Description: "Optional maximum number of recent messages to return.", Validation: map[string]any{"min": 0}},
				},
				Notes: []string{
					"The response shape is {ok, user, recent_messages}.",
				},
			},
			{
				Name:          "list_user_identities",
				Method:        "GET",
				Path:          "/api/admin/users/{user_id}/identities",
				Description:   "List external identities linked to a specific user.",
				RequiresAdmin: true,
				Notes: []string{
					"The response shape is {ok, user, count, items}.",
				},
			},
			{
				Name:          "preview_user_purge",
				Method:        "GET",
				Path:          "/api/admin/users/{user_id}/delete-preview",
				Description:   "Preview whether a user can be physically deleted and return dependency counts.",
				RequiresAdmin: true,
				Notes: []string{
					"The response shape is {ok, user, preview}.",
					"owned_conversations and owned_uploaded_images currently block physical deletion because their ownership history is preserved.",
				},
			},
			{
				Name:          "reset_user_password",
				Method:        "PUT",
				Path:          "/api/admin/users/{user_id}/password",
				Description:   "Reset the password of an existing local user.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "password", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "New password to hash and store for the target user."},
				},
			},
			{
				Name:          "unlink_user_identity",
				Method:        "DELETE",
				Path:          "/api/admin/users/{user_id}/identities/{identity_id}",
				Description:   "Unlink one external identity from a specific user.",
				RequiresAdmin: true,
				Notes: []string{
					"Serve mode requires a valid same-origin session mutation request.",
					"If the target account has no local password and this is the last login method, the endpoint returns 409.",
				},
			},
			{
				Name:          "deactivate_user",
				Method:        "DELETE",
				Path:          "/api/admin/users/{user_id}",
				Description:   "Deactivate a local user without deleting their historical ownership records.",
				RequiresAdmin: true,
				Notes: []string{
					"This endpoint marks is_active=false instead of physically deleting the user row.",
				},
			},
			{
				Name:          "purge_user",
				Method:        "POST",
				Path:          "/api/admin/users/{user_id}/purge",
				Description:   "Physically delete a user account after a clean delete-preview with no history blockers.",
				RequiresAdmin: true,
				Notes: []string{
					"This endpoint deletes identities, user configs, automation rules, app/model API keys and storage quota rows for the target user.",
					"If preview.can_delete is false, the endpoint returns 409 together with the preview payload.",
					"Audit logs are intentionally preserved for operations visibility.",
				},
			},
		},
	}
}
