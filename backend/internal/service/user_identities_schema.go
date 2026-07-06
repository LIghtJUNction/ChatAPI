package service

type UserIdentitiesSchema struct {
	Operations []UserIdentitiesOperationSchema `json:"operations"`
}

type UserIdentitiesOperationSchema struct {
	Name         string              `json:"name"`
	Method       string              `json:"method"`
	Path         string              `json:"path"`
	Description  string              `json:"description"`
	Fields       []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresUser bool                `json:"requires_user_actor"`
	Notes        []string            `json:"notes,omitempty"`
}

func BuildUserIdentitiesSchema() UserIdentitiesSchema {
	return UserIdentitiesSchema{
		Operations: []UserIdentitiesOperationSchema{
			{
				Name:         "list_identities",
				Method:       "GET",
				Path:         "/api/user/identities",
				Description:  "List external identities linked to the current interactive user.",
				RequiresUser: true,
				Notes: []string{
					"The response shape is {items, count}.",
				},
			},
			{
				Name:         "link_identity",
				Method:       "GET",
				Path:         "/api/auth/oidc/link",
				Description:  "Start an OIDC provider flow that links the provider account to the current interactive user.",
				RequiresUser: true,
				Notes: []string{
					"Serve mode requires an existing session and redirects the browser to the configured OIDC provider.",
				},
			},
			{
				Name:         "unlink_identity",
				Method:       "DELETE",
				Path:         "/api/user/identities/{identity_id}",
				Description:  "Unlink one external identity from the current interactive user.",
				RequiresUser: true,
				Notes: []string{
					"Serve mode requires a valid same-origin session mutation request.",
					"If the account has no local password and this is the last login method, the endpoint returns 409.",
				},
			},
		},
	}
}
