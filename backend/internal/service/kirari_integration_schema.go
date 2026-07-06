package service

type KirariIntegrationSchema struct {
	Operations []KirariIntegrationOperationSchema `json:"operations"`
}

type KirariIntegrationOperationSchema struct {
	Name         string              `json:"name"`
	Method       string              `json:"method"`
	Path         string              `json:"path"`
	Description  string              `json:"description"`
	Fields       []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresUser bool                `json:"requires_user_actor"`
	Notes        []string            `json:"notes,omitempty"`
}

func BuildKirariIntegrationSchema() KirariIntegrationSchema {
	return KirariIntegrationSchema{
		Operations: []KirariIntegrationOperationSchema{
			{
				Name:         "get_status",
				Method:       "GET",
				Path:         "/api/user/integrations/kirari",
				Description:  "Read the current user's Kirari delegated upstream connection status.",
				RequiresUser: true,
				Notes: []string{
					"The response shape is {ok, status}.",
					"No tokens are returned to the browser; only subject, scopes, expiry and cached model summary metadata are exposed.",
				},
			},
			{
				Name:         "connect",
				Method:       "GET",
				Path:         "/api/user/integrations/kirari/connect",
				Description:  "Start the Kirari authorization code + PKCE flow for the current interactive user.",
				RequiresUser: true,
				Notes: []string{
					"The endpoint redirects the browser to the configured Kirari issuer.",
				},
			},
			{
				Name:         "callback",
				Method:       "GET",
				Path:         "/api/integrations/kirari/callback",
				Description:  "Complete the Kirari authorization code flow and store encrypted delegated tokens for the current user.",
				RequiresUser: true,
				Fields: []ConfigFieldSchema{
					{Key: "code", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Authorization code returned by Kirari."},
					{Key: "state", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "State value returned by Kirari; must match the connect cookie."},
				},
			},
			{
				Name:         "disconnect",
				Method:       "DELETE",
				Path:         "/api/user/integrations/kirari",
				Description:  "Delete the current user's encrypted Kirari delegated token set and cached model metadata.",
				RequiresUser: true,
			},
			{
				Name:         "get_meta",
				Method:       "GET",
				Path:         "/api/user/integrations/kirari/meta",
				Description:  "Read delegated Kirari model metadata for the current user, optionally bypassing cache.",
				RequiresUser: true,
				Fields: []ConfigFieldSchema{
					{Key: "force_refresh", ValueType: "boolean", DefaultValue: false, Public: false, AdminWriteOnly: true, Description: "When true, bypass cached model metadata and fetch a fresh delegated /api/llm/meta response."},
				},
				Notes: []string{
					"The response shape is {ok, cached, meta}.",
				},
			},
		},
	}
}
