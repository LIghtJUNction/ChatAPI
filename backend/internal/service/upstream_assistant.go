package service

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

type UpstreamAssistantSchema struct {
	Fields          []ConfigFieldSchema `json:"fields"`
	ProtocolOptions []string            `json:"protocol_options"`
	SensitiveFields []string            `json:"sensitive_fields"`
	DefaultConfig   map[string]any      `json:"default_config"`
	ValidationRules []string            `json:"validation_rules"`
	ErrorCodes      []UpstreamErrorCode `json:"error_codes"`
	Storage         map[string]any      `json:"storage"`
}

type UpstreamErrorCode struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

type UpstreamProtocolTemplate struct {
	Protocol           string         `json:"protocol"`
	DefaultPath        string         `json:"default_path"`
	Method             string         `json:"method"`
	AuthHeaderTemplate string         `json:"auth_header_template,omitempty"`
	RequestShape       map[string]any `json:"request_shape"`
	RequestHeaders     map[string]any `json:"request_headers,omitempty"`
	ConstructionTips   []string       `json:"construction_tips"`
	ResponseExtraction map[string]any `json:"response_extraction,omitempty"`
}

type UpstreamAssistantHints struct {
	CurrentInstanceURLs []string `json:"current_instance_urls"`
	CandidateBaseURL    string   `json:"candidate_base_url,omitempty"`
	CandidateRecursive  bool     `json:"candidate_recursive"`
	Warnings            []string `json:"warnings,omitempty"`
	Notes               []string `json:"notes,omitempty"`
}

type UpstreamInputHints struct {
	DefaultMaxInputMessages int              `json:"default_max_input_messages"`
	AvailableMessages       int              `json:"available_messages"`
	RecommendedMessages     []map[string]any `json:"recommended_messages"`
	Truncated               bool             `json:"truncated"`
	ExcludedMessages        int              `json:"excluded_messages"`
	ConstructionRules       []string         `json:"construction_rules"`
}

func BuildUpstreamAssistantSchema() UpstreamAssistantSchema {
	return UpstreamAssistantSchema{
		Fields: []ConfigFieldSchema{
			{Key: "enabled", ValueType: "boolean", DefaultValue: false, Public: true, Description: "Whether upstream assistant is enabled in this browser."},
			{Key: "protocol", ValueType: "string", DefaultValue: "responses", Public: true, Description: "Upstream protocol used by the browser assistant.", Validation: map[string]any{"allowed_values": []string{"responses", "chat_completions", "anthropic_messages"}}},
			{Key: "base_url", ValueType: "string", DefaultValue: "", Public: true, Description: "Base URL of the upstream model API."},
			{Key: "api_key", ValueType: "string", DefaultValue: "", Public: true, Description: "API key stored only in the browser.", Validation: map[string]any{"sensitive": true}},
			{Key: "model", ValueType: "string", DefaultValue: "", Public: true, Description: "Upstream model name."},
			{Key: "extra_headers", ValueType: "object", DefaultValue: map[string]any{}, Public: true, Description: "Optional extra headers sent by the browser."},
			{Key: "timeout_seconds", ValueType: "integer", DefaultValue: 30, Public: true, Description: "Browser-side request timeout in seconds.", Validation: map[string]any{"min": 1, "max": 600}},
			{Key: "max_input_messages", ValueType: "integer", DefaultValue: 20, Public: true, Description: "Maximum context messages included in one upstream request.", Validation: map[string]any{"min": 1, "max": 200}},
		},
		ProtocolOptions: []string{"responses", "chat_completions", "anthropic_messages"},
		SensitiveFields: []string{"api_key"},
		DefaultConfig: map[string]any{
			"enabled":            false,
			"protocol":           "responses",
			"base_url":           "",
			"api_key":            "",
			"model":              "",
			"extra_headers":      map[string]any{},
			"timeout_seconds":    30,
			"max_input_messages": 20,
		},
		ValidationRules: []string{
			"When enabled=true, protocol, base_url, api_key, and model must all be non-empty.",
			"base_url must be an absolute http or https URL.",
			"extra_headers must remain a flat JSON object with string values.",
			"timeout_seconds must stay within [1, 600].",
			"max_input_messages must stay within [1, 200].",
			"candidate_base_url should be checked against upstream_hints before the UI sends a real upstream request.",
		},
		ErrorCodes: []UpstreamErrorCode{
			{Code: "upstream_assistant.invalid_protocol", Description: "protocol is not one of responses, chat_completions, anthropic_messages"},
			{Code: "upstream_assistant.invalid_base_url", Description: "base_url is not a valid absolute http/https URL"},
			{Code: "upstream_assistant.recursive_base_url", Description: "base_url points to the current ChatAPI instance and may recurse"},
			{Code: "upstream_assistant.invalid_extra_headers", Description: "extra_headers contains non-string values or nested objects"},
			{Code: "upstream_assistant.timeout_out_of_range", Description: "timeout_seconds is outside the allowed range"},
			{Code: "upstream_assistant.max_input_messages_out_of_range", Description: "max_input_messages is outside the allowed range"},
			{Code: "upstream_assistant.missing_required_field", Description: "enabled assistant is missing a required non-sensitive or sensitive field"},
		},
		Storage: map[string]any{
			"mode":                 "browser_local_only",
			"allow_server_storage": false,
		},
	}
}

func BuildUpstreamProtocolTemplates() []UpstreamProtocolTemplate {
	return []UpstreamProtocolTemplate{
		{
			Protocol:           "responses",
			DefaultPath:        "/v1/responses",
			Method:             "POST",
			AuthHeaderTemplate: "Authorization: Bearer $config.api_key",
			RequestShape: map[string]any{
				"model":             "$config.model",
				"input":             "$recommended_messages",
				"tools":             "$parsed.normalized_tool_schemas",
				"response_format":   "$assist_schema.output_json_schema",
				"instructions":      "$optional.explanation_prompt",
				"max_output_tokens": "$optional.max_output_tokens",
				"stream":            false,
			},
			RequestHeaders: map[string]any{
				"Content-Type": "application/json",
			},
			ConstructionTips: []string{
				"Map recommended_messages into Responses input message items.",
				"Pass normalized tool schemas directly as tools when the upstream supports OpenAI-style function tools.",
				"Keep stream=false for the first browser-side assistant implementation.",
			},
			ResponseExtraction: map[string]any{
				"mode": "final_text",
				"primary_paths": []string{
					"output_text",
					"output[0].content[0].text",
				},
				"notes": []string{
					"If the upstream returns native structured output, still preserve the raw text form for assist_parse fallback.",
				},
			},
		},
		{
			Protocol:           "chat_completions",
			DefaultPath:        "/v1/chat/completions",
			Method:             "POST",
			AuthHeaderTemplate: "Authorization: Bearer $config.api_key",
			RequestShape: map[string]any{
				"model":       "$config.model",
				"messages":    "$recommended_messages",
				"tools":       "$parsed.normalized_tool_schemas",
				"tool_choice": "auto",
				"response_format": map[string]any{
					"type":        "json_schema",
					"json_schema": "$assist_schema.output_json_schema",
				},
				"stream": false,
			},
			RequestHeaders: map[string]any{
				"Content-Type": "application/json",
			},
			ConstructionTips: []string{
				"Convert recommended_messages into Chat Completions messages preserving role and content order.",
				"Inject the explanation/JSON output instruction as a system or developer message if the upstream requires explicit prompting.",
				"Use normalized_tool_schemas as the source of tools instead of raw request tool payloads.",
			},
			ResponseExtraction: map[string]any{
				"mode": "chat_completions_message",
				"primary_paths": []string{
					"choices[0].message.content",
					"choices[0].delta.content",
				},
				"stream_delta_paths": []string{
					"choices[0].delta.content",
					"choices[0].delta.content[].text",
				},
			},
		},
		{
			Protocol:           "anthropic_messages",
			DefaultPath:        "/messages",
			Method:             "POST",
			AuthHeaderTemplate: "x-api-key: $config.api_key",
			RequestShape: map[string]any{
				"model":      "$config.model",
				"messages":   "$recommended_messages",
				"tools":      "$parsed.normalized_tool_schemas",
				"system":     "$optional.explanation_prompt",
				"max_tokens": "$optional.max_output_tokens",
				"stream":     false,
			},
			RequestHeaders: map[string]any{
				"Content-Type":      "application/json",
				"anthropic-version": "2023-06-01",
			},
			ConstructionTips: []string{
				"Convert recommended_messages into Anthropic messages with text blocks.",
				"Translate normalized_tool_schemas into Anthropic tool definitions with input_schema.",
				"Ask the upstream to emit explanation text plus JSON in plain assistant output, then validate against assist_schema client-side.",
			},
			ResponseExtraction: map[string]any{
				"mode": "anthropic_message_text",
				"primary_paths": []string{
					"content[0].text",
				},
				"stream_delta_paths": []string{
					"delta.text",
					"content_block_delta.delta.text",
				},
				"notes": []string{
					"Anthropic plain text mode should still emit explanation plus one JSON object in assistant text.",
				},
			},
		},
	}
}

func BuildUpstreamAssistantHints(cfg config.Config, observedBaseURL string, candidateBaseURL string) UpstreamAssistantHints {
	currentURLs := currentInstanceURLs(cfg, observedBaseURL)
	candidateBaseURL = strings.TrimSpace(candidateBaseURL)
	hints := UpstreamAssistantHints{
		CurrentInstanceURLs: currentURLs,
		CandidateBaseURL:    candidateBaseURL,
		CandidateRecursive:  false,
		Notes: []string{
			"Browser CORS capability still depends on the upstream server and cannot be guaranteed by ChatAPI.",
			"Keep upstream API keys in browser-local storage only.",
		},
	}
	if candidateBaseURL == "" {
		return hints
	}
	if _, err := url.Parse(candidateBaseURL); err != nil {
		hints.Warnings = append(hints.Warnings, "candidate_base_url is not a valid URL")
		return hints
	}
	if isRecursiveCandidate(currentURLs, candidateBaseURL) {
		hints.CandidateRecursive = true
		hints.Warnings = append(hints.Warnings, "candidate_base_url points to the current ChatAPI instance and may recurse back into pending turns")
	}
	return hints
}

func BuildUpstreamInputHints(messages []store.Message, draftText string, maxInputMessages int) UpstreamInputHints {
	if maxInputMessages <= 0 {
		maxInputMessages = 20
	}
	selected := messages
	truncated := false
	excluded := 0
	if len(selected) > maxInputMessages {
		truncated = true
		excluded = len(selected) - maxInputMessages
		selected = selected[len(selected)-maxInputMessages:]
	}
	recommended := make([]map[string]any, 0, len(selected))
	for _, item := range selected {
		recommended = append(recommended, map[string]any{
			"id":         item.ID,
			"role":       item.Role,
			"content":    item.Content,
			"created_at": item.CreatedAt,
			"status":     item.Status,
		})
	}
	rules := []string{
		"Use recommended_messages as the default upstream context window in chronological order.",
		"If truncated=true, older messages were dropped by count only; no token-based truncation has been applied yet.",
		"Do not convert the current draft into a committed assistant message automatically.",
	}
	if strings.TrimSpace(draftText) != "" {
		rules = append(rules, "If the UI wants the upstream model to see the current draft, pass draft.text separately instead of mutating recommended_messages.")
	}
	return UpstreamInputHints{
		DefaultMaxInputMessages: maxInputMessages,
		AvailableMessages:       len(messages),
		RecommendedMessages:     recommended,
		Truncated:               truncated,
		ExcludedMessages:        excluded,
		ConstructionRules:       rules,
	}
}

func currentInstanceURLs(cfg config.Config, observedBaseURL string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if _, ok := seen[raw]; ok {
			return
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		add(strings.TrimRight(baseURL, "/"))
	}
	if observedBaseURL != "" {
		add(strings.TrimRight(strings.TrimSpace(observedBaseURL), "/"))
	}
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return out
	}
	if host == "0.0.0.0" {
		add(fmt.Sprintf("http://127.0.0.1:%d", cfg.Port))
		add(fmt.Sprintf("http://localhost:%d", cfg.Port))
		return out
	}
	add(fmt.Sprintf("http://%s:%d", host, cfg.Port))
	if host == "127.0.0.1" {
		add(fmt.Sprintf("http://localhost:%d", cfg.Port))
	}
	if host == "localhost" {
		add(fmt.Sprintf("http://127.0.0.1:%d", cfg.Port))
	}
	return out
}

func isRecursiveCandidate(currentURLs []string, candidate string) bool {
	candidateURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(candidate), "/"))
	if err != nil {
		return false
	}
	for _, raw := range currentURLs {
		currentURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
		if err != nil {
			continue
		}
		if strings.EqualFold(currentURL.Scheme, candidateURL.Scheme) &&
			strings.EqualFold(currentURL.Host, candidateURL.Host) {
			return true
		}
	}
	return false
}
