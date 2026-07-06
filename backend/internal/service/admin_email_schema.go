package service

type AdminEmailSchema struct {
	Operations []AdminEmailOperationSchema `json:"operations"`
}

type AdminEmailOperationSchema struct {
	Name          string              `json:"name"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	Description   string              `json:"description"`
	Fields        []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresAdmin bool                `json:"requires_admin"`
	Notes         []string            `json:"notes,omitempty"`
}

func BuildAdminEmailSchema() AdminEmailSchema {
	return AdminEmailSchema{
		Operations: []AdminEmailOperationSchema{
			{
				Name:          "send_test_email",
				Method:        "POST",
				Path:          "/api/admin/send-test-email",
				Description:   "Send a test email using the current process SMTP runtime configuration.",
				RequiresAdmin: true,
				Fields: []ConfigFieldSchema{
					{Key: "email", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Recipient email address for the test message."},
				},
				Notes: []string{
					"This endpoint reads SMTP settings from the current runtime environment, not from persisted provider credentials.",
					"Configuration problems return 400; downstream SMTP delivery failures return 502.",
				},
			},
		},
	}
}
