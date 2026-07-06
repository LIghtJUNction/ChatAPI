package service

type AdminRequestsSchema struct {
	Operations []AdminRequestsOperationSchema `json:"operations"`
}

type AdminRequestsOperationSchema struct {
	Name          string   `json:"name"`
	Method        string   `json:"method"`
	Path          string   `json:"path"`
	Description   string   `json:"description"`
	RequiresAdmin bool     `json:"requires_admin"`
	Notes         []string `json:"notes,omitempty"`
}

func BuildAdminRequestsSchema() AdminRequestsSchema {
	return AdminRequestsSchema{
		Operations: []AdminRequestsOperationSchema{
			{
				Name:          "requests_overview",
				Method:        "GET",
				Path:          "/api/admin/requests/overview",
				Description:   "Read a global request overview aggregated across all owners and virtual models.",
				RequiresAdmin: true,
				Notes: []string{
					"The response shape is {ok, overview}.",
					"overview includes total_requests, pending_requests, streaming_requests, closed_requests, aborted_requests, automation_hits, by_status, by_model, by_owner and generated_at.",
					"oldest_pending_wait_seconds is omitted when there are no waiting or streaming requests.",
				},
			},
		},
	}
}
