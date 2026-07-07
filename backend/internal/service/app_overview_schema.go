package service

type AppOverviewSchema struct {
	Authentication AppAPIAuthenticationSchema   `json:"authentication,omitempty"`
	Operations     []AppOverviewOperationSchema `json:"operations"`
	ErrorCodes     []AppAPIErrorCodeSchema      `json:"error_codes,omitempty"`
}

type AppOverviewOperationSchema struct {
	Name              string   `json:"name"`
	Method            string   `json:"method"`
	Path              string   `json:"path"`
	Description       string   `json:"description"`
	RequiredScopes    []string `json:"required_scopes,omitempty"`
	ResourceLimitKeys []string `json:"resource_limit_keys,omitempty"`
	ErrorCodes        []string `json:"error_codes,omitempty"`
	ResponseShape     string   `json:"response_shape,omitempty"`
	ConsumesRateLimit bool     `json:"consumes_rate_limit,omitempty"`
	Notes             []string `json:"notes,omitempty"`
}

func BuildAppOverviewSchema() AppOverviewSchema {
	return AppOverviewSchema{
		Authentication: BuildAppAPIAuthenticationSchema(),
		Operations: []AppOverviewOperationSchema{
			{
				Name:              "me",
				Method:            "GET",
				Path:              "/api/app/me",
				Description:       "Read the current app API key metadata and its owner id.",
				RequiredScopes:    []string{"requests:read"},
				ErrorCodes:        appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "internal_error"),
				ResponseShape:     "{ok, app_api_key, user}",
				ConsumesRateLimit: true,
				Notes: []string{
					"The response shape is {ok, app_api_key, user}.",
				},
			},
			{
				Name:              "statistics_summary",
				Method:            "GET",
				Path:              "/api/app/statistics/summary",
				Description:       "Read request statistics summary for the current app API key owner.",
				RequiredScopes:    []string{"statistics:read"},
				ErrorCodes:        appAPIErrorCodeList("app_api_key_unauthorized", "source_ip_forbidden", "forbidden", "rate_limited", "internal_error"),
				ResponseShape:     "{ok, summary}",
				ConsumesRateLimit: true,
				Notes: []string{
					"The response shape is {ok, summary}.",
					"summary includes total_requests, pending_requests, streaming_requests, closed_requests, aborted_requests, automation_hits, by_status, by_model and generated_at.",
				},
			},
		},
		ErrorCodes: BuildCommonAppAPIErrorCodes(),
	}
}
