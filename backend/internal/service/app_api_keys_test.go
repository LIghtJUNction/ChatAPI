package service

import (
	"strings"
	"testing"
)

func TestAppAPIKeyValidateConfig(t *testing.T) {
	svc := NewAppAPIKeyService(nil)

	cases := []struct {
		name           string
		scopes         []string
		resourceLimits map[string]any
		wantErr        string
	}{
		{
			name:    "missing_scopes",
			scopes:  nil,
			wantErr: "scopes are required",
		},
		{
			name:    "unknown_scope",
			scopes:  []string{"requests:read", "unknown:scope"},
			wantErr: "unsupported app api key scope",
		},
		{
			name:   "invalid_allowed_request_action_without_scope",
			scopes: []string{"requests:read"},
			resourceLimits: map[string]any{
				"allowed_request_actions": []any{"complete"},
			},
			wantErr: "requires requests:respond scope",
		},
		{
			name:   "invalid_allowed_request_action_value",
			scopes: []string{"requests:respond"},
			resourceLimits: map[string]any{
				"allowed_request_actions": []any{"publish"},
			},
			wantErr: "unsupported allowed_request_action",
		},
		{
			name:   "invalid_allowed_source_ip",
			scopes: []string{"requests:read"},
			resourceLimits: map[string]any{
				"allowed_source_ips": []any{"not-an-ip"},
			},
			wantErr: "invalid allowed_source_ips entry",
		},
		{
			name:   "invalid_positive_integer",
			scopes: []string{"model_keys:write"},
			resourceLimits: map[string]any{
				"max_model_keys": 0,
			},
			wantErr: "must be a positive integer",
		},
		{
			name:   "unknown_resource_limit",
			scopes: []string{"requests:read"},
			resourceLimits: map[string]any{
				"unexpected_limit": true,
			},
			wantErr: "unsupported app api key resource limit",
		},
		{
			name:   "valid_config",
			scopes: []string{"requests:read", "requests:respond", "requests:read"},
			resourceLimits: map[string]any{
				"allowed_request_ids":      []any{"req_1", "req_1", "req_2"},
				"allowed_conversation_ids": []any{"conv_1"},
				"allowed_request_actions":  []any{"complete", "delta"},
				"allowed_source_ips":       []any{"127.0.0.1", "10.0.0.0/8"},
				"max_requests_per_minute":  5,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.ValidateConfig(tc.scopes, tc.resourceLimits)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
