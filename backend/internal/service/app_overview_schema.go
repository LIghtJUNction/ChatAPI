package service

type AppOverviewSchema struct {
	Operations []AppOverviewOperationSchema `json:"operations"`
}

type AppOverviewOperationSchema struct {
	Name           string   `json:"name"`
	Method         string   `json:"method"`
	Path           string   `json:"path"`
	Description    string   `json:"description"`
	RequiredScopes []string `json:"required_scopes,omitempty"`
	Notes          []string `json:"notes,omitempty"`
}

func BuildAppOverviewSchema() AppOverviewSchema {
	return AppOverviewSchema{
		Operations: []AppOverviewOperationSchema{
			{
				Name:           "me",
				Method:         "GET",
				Path:           "/api/app/me",
				Description:    "Read the current app API key metadata and its owner id.",
				RequiredScopes: []string{"requests:read"},
				Notes: []string{
					"The response shape is {ok, app_api_key, user}.",
				},
			},
			{
				Name:           "statistics_summary",
				Method:         "GET",
				Path:           "/api/app/statistics/summary",
				Description:    "Read request statistics summary for the current app API key owner.",
				RequiredScopes: []string{"statistics:read"},
				Notes: []string{
					"The response shape is {ok, summary}.",
					"summary includes total_requests, pending_requests, streaming_requests, closed_requests, aborted_requests, automation_hits, by_status, by_model and generated_at.",
				},
			},
		},
	}
}
