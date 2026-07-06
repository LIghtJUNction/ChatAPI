package service

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/zyf/chatapi/internal/config"
)

type UpstreamAssistantSchema struct {
	Fields          []ConfigFieldSchema `json:"fields"`
	ProtocolOptions []string            `json:"protocol_options"`
	SensitiveFields []string            `json:"sensitive_fields"`
	Storage         map[string]any      `json:"storage"`
}

type UpstreamAssistantHints struct {
	CurrentInstanceURLs []string `json:"current_instance_urls"`
	CandidateBaseURL    string   `json:"candidate_base_url,omitempty"`
	CandidateRecursive  bool     `json:"candidate_recursive"`
	Warnings            []string `json:"warnings,omitempty"`
	Notes               []string `json:"notes,omitempty"`
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
		Storage: map[string]any{
			"mode":                 "browser_local_only",
			"allow_server_storage": false,
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
