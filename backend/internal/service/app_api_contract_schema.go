package service

type AppAPIAuthenticationSchema struct {
	Headers []AppAPIAuthHeaderSchema `json:"headers"`
	Notes   []string                 `json:"notes,omitempty"`
}

type AppAPIAuthHeaderSchema struct {
	Name        string `json:"name"`
	Scheme      string `json:"scheme,omitempty"`
	Example     string `json:"example,omitempty"`
	Description string `json:"description,omitempty"`
}

type AppAPIErrorCodeSchema struct {
	Code        string `json:"code"`
	HTTPStatus  int    `json:"http_status"`
	Description string `json:"description"`
	Retryable   bool   `json:"retryable,omitempty"`
}

type AppAPIOperationContract struct {
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

type AppAPIResourceLimitBindingSchema struct {
	Name              string   `json:"name"`
	AffectsOperations []string `json:"affects_operations,omitempty"`
	Behavior          string   `json:"behavior"`
}

func BuildAppAPIAuthenticationSchema() AppAPIAuthenticationSchema {
	return AppAPIAuthenticationSchema{
		Headers: []AppAPIAuthHeaderSchema{
			{
				Name:        "X-ChatAPI-App-Key",
				Example:     "ak-...",
				Description: "Preferred app API key header.",
			},
			{
				Name:        "Authorization",
				Scheme:      "Bearer",
				Example:     "Bearer ak-...",
				Description: "Alternative bearer-token transport for the same app API key.",
			},
		},
		Notes: []string{
			"All /api/app/* routes are authenticated with an application API key, not with a browser session cookie.",
			"allowed_source_ips is evaluated before scope checks when the request is authenticated.",
			"max_requests_per_minute is enforced as a per-key one-minute window across all /api/app/* routes.",
		},
	}
}

func BuildCommonAppAPIErrorCodes() []AppAPIErrorCodeSchema {
	return []AppAPIErrorCodeSchema{
		{Code: "app_api_key_unauthorized", HTTPStatus: 401, Description: "The application API key is missing, expired, revoked, malformed, or does not match any stored key."},
		{Code: "source_ip_forbidden", HTTPStatus: 403, Description: "The request source IP does not match allowed_source_ips."},
		{Code: "forbidden", HTTPStatus: 403, Description: "The application API key is authenticated but missing the required scope or resource access."},
		{Code: "rate_limited", HTTPStatus: 429, Description: "The application API key exceeded max_requests_per_minute.", Retryable: true},
		{Code: "invalid_json_body", HTTPStatus: 400, Description: "The request body is not valid JSON for this operation."},
		{Code: "invalid_request", HTTPStatus: 400, Description: "The request body or path parameters fail service-side validation."},
		{Code: "not_found", HTTPStatus: 404, Description: "The requested resource does not exist or is not owned by the app API key owner."},
		{Code: "pending_not_found", HTTPStatus: 404, Description: "The target pending request state no longer exists for a turn-control operation."},
		{Code: "pending_conflict", HTTPStatus: 409, Description: "The pending turn state changed before the requested mutation completed.", Retryable: true},
		{Code: "internal_error", HTTPStatus: 500, Description: "Unexpected backend failure while serving the application API request.", Retryable: true},
	}
}

func appAPIErrorCodeList(codes ...string) []string {
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		if code != "" {
			out = append(out, code)
		}
	}
	return out
}
