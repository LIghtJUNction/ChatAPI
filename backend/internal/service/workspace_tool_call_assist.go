package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zyf/chatapi/internal/protocol"
)

var (
	ErrToolCallAssistProviderRequired = errors.New("assist provider is required")
	ErrToolCallAssistModelRequired    = errors.New("assist model is required")
	ErrToolCallAssistUnsupported      = errors.New("assist provider is not supported")
	ErrToolCallAssistNoTools          = errors.New("assist target does not declare any tools")
	ErrToolCallAssistInvalidOutput    = errors.New("assist output is invalid")
)

type ToolCallAssistResult struct {
	Provider         string         `json:"provider"`
	Model            string         `json:"model"`
	Explanation      string         `json:"explanation"`
	ToolCall         map[string]any `json:"tool_call,omitempty"`
	Confidence       string         `json:"confidence,omitempty"`
	Warnings         []string       `json:"warnings,omitempty"`
	ValidationErrors []string       `json:"validation_errors,omitempty"`
	ValidDraft       bool           `json:"valid_draft"`
	RawOutput        string         `json:"raw_output,omitempty"`
	Request          map[string]any `json:"request,omitempty"`
}

type ToolCallAssistService struct {
	workspace *WorkspaceToolCallService
	kirari    *KirariIntegrationService
}

func NewToolCallAssistService(workspace *WorkspaceToolCallService, kirari *KirariIntegrationService) *ToolCallAssistService {
	return &ToolCallAssistService{
		workspace: workspace,
		kirari:    kirari,
	}
}

func (s *ToolCallAssistService) Execute(ctx context.Context, userID string, provider string, model string, requestID string, conversationID string) (ToolCallAssistResult, error) {
	if s == nil || s.workspace == nil {
		return ToolCallAssistResult{}, ErrForbidden
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)
	if provider == "" {
		return ToolCallAssistResult{}, ErrToolCallAssistProviderRequired
	}
	if model == "" {
		return ToolCallAssistResult{}, ErrToolCallAssistModelRequired
	}
	contextPayload, err := s.workspace.AssistContext(ctx, userID, requestID, conversationID)
	if err != nil {
		return ToolCallAssistResult{}, err
	}
	switch provider {
	case "kirari":
		return s.executeKirari(ctx, userID, model, contextPayload)
	default:
		return ToolCallAssistResult{}, ErrToolCallAssistUnsupported
	}
}

func (s *ToolCallAssistService) executeKirari(ctx context.Context, userID string, model string, contextPayload ToolCallAssistContext) (ToolCallAssistResult, error) {
	if s.kirari == nil {
		return ToolCallAssistResult{}, ErrToolCallAssistUnsupported
	}
	normalizedTools := protocol.NormalizeToolSchemas(contextPayload.Request.ToolSchemas)
	if len(normalizedTools) == 0 {
		return ToolCallAssistResult{}, ErrToolCallAssistNoTools
	}
	body := map[string]any{
		"model":    model,
		"stream":   false,
		"messages": buildKirariAssistMessages(contextPayload, normalizedTools, s.workspace.AssistSchema()),
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "tool_call_assist",
				"schema": s.workspace.AssistSchema().OutputJSONSchema,
			},
		},
	}
	payload, err := s.kirari.ChatCompletions(ctx, userID, body)
	if err != nil {
		return ToolCallAssistResult{}, err
	}
	result := ToolCallAssistResult{
		Provider: providerKirari,
		Model:    model,
		Request: map[string]any{
			"request_id":      contextPayload.Request.RequestID,
			"conversation_id": contextPayload.Request.ConversationID,
		},
	}
	content := firstChatCompletionContent(payload)
	result.RawOutput = content
	if strings.TrimSpace(content) == "" {
		result.ValidationErrors = []string{"assistant output is empty"}
		return result, nil
	}
	parsed, err := decodeAssistOutput(content)
	if err != nil {
		result.Explanation = strings.TrimSpace(content)
		result.ValidationErrors = []string{err.Error()}
		return result, nil
	}
	result.Explanation = stringValueFromMap(parsed, "explanation")
	result.ToolCall = objectValueFromMap(parsed, "tool_call")
	result.Confidence = stringValueFromMap(parsed, "confidence")
	result.Warnings = stringArrayValueFromMap(parsed, "warnings")
	result.ValidationErrors = validateAssistDraft(result.ToolCall, normalizedTools)
	result.ValidDraft = len(result.ValidationErrors) == 0
	return result, nil
}

const providerKirari = "kirari"

func buildKirariAssistMessages(contextPayload ToolCallAssistContext, tools []protocol.NormalizedToolSchema, schema ToolCallAssistSchema) []map[string]any {
	messages := make([]map[string]any, 0, len(contextPayload.Messages)+3)
	systemPrompt := map[string]any{
		"role": "system",
		"content": strings.TrimSpace(strings.Join([]string{
			"You are assisting a human operator who is preparing a tool call draft.",
			"Choose exactly one tool from the provided tool list and produce JSON that matches the required schema.",
			"The JSON must include explanation before the draft via the explanation field.",
			"Do not invent tools that are not declared.",
			"Do not assume missing arguments when the context is ambiguous; mention uncertainty in explanation or warnings.",
			"Required output schema:",
			mustJSONString(schema.OutputJSONSchema),
			"Available tools:",
			mustJSONString(tools),
		}, "\n")),
	}
	messages = append(messages, systemPrompt)
	for _, item := range BuildUpstreamInputHints(contextPayload.Messages, contextPayload.DraftText, 20).RecommendedMessages {
		role := stringValueAny(item["role"])
		content := stringValueAny(item["content"])
		if strings.TrimSpace(role) == "" || strings.TrimSpace(content) == "" {
			continue
		}
		messages = append(messages, map[string]any{
			"role":    role,
			"content": content,
		})
	}
	if strings.TrimSpace(contextPayload.DraftText) != "" {
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": "Current draft text from the workspace (not yet submitted):\n" + contextPayload.DraftText,
		})
	}
	messages = append(messages, map[string]any{
		"role": "user",
		"content": strings.TrimSpace(strings.Join([]string{
			"Return only one JSON object matching the schema.",
			"Use the explanation field to describe why the selected tool and arguments are appropriate.",
			"If no safe draft is possible, still return explanation and warnings, and leave tool_call arguments as an empty object for the best matching tool.",
		}, "\n")),
	})
	return messages
}

func firstChatCompletionContent(payload map[string]any) string {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	firstChoice, _ := choices[0].(map[string]any)
	message, _ := firstChoice["message"].(map[string]any)
	switch content := message["content"].(type) {
	case string:
		return strings.TrimSpace(content)
	case []any:
		parts := make([]string, 0, len(content))
		for _, item := range content {
			block, _ := item.(map[string]any)
			if block == nil {
				continue
			}
			text := stringValueAny(block["text"])
			if text == "" {
				text = stringValueAny(block["content"])
			}
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}

func decodeAssistOutput(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrToolCallAssistInvalidOutput
	}
	if parsed, err := decodeJSONObject(raw); err == nil {
		return parsed, nil
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		if parsed, err := decodeJSONObject(raw[start : end+1]); err == nil {
			return parsed, nil
		}
	}
	return nil, ErrToolCallAssistInvalidOutput
}

func decodeJSONObject(raw string) (map[string]any, error) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	if parsed == nil {
		return nil, ErrToolCallAssistInvalidOutput
	}
	return parsed, nil
}

func validateAssistDraft(toolCall map[string]any, tools []protocol.NormalizedToolSchema) []string {
	errorsOut := make([]string, 0, 3)
	if toolCall == nil {
		return append(errorsOut, "tool_call is required")
	}
	name := stringValueAny(toolCall["name"])
	if name == "" {
		errorsOut = append(errorsOut, "tool_call.name is required")
	}
	if _, ok := toolCall["arguments"].(map[string]any); !ok {
		errorsOut = append(errorsOut, "tool_call.arguments must be an object")
	}
	if name != "" {
		found := false
		for _, item := range tools {
			if strings.TrimSpace(item.Name) == name {
				found = true
				break
			}
		}
		if !found {
			errorsOut = append(errorsOut, fmt.Sprintf("tool_call.name %q is not declared in the current tool schemas", name))
		}
	}
	return errorsOut
}

func stringValueFromMap(record map[string]any, key string) string {
	if record == nil {
		return ""
	}
	return stringValueAny(record[key])
}

func objectValueFromMap(record map[string]any, key string) map[string]any {
	if record == nil {
		return nil
	}
	value, _ := record[key].(map[string]any)
	return value
}

func stringArrayValueFromMap(record map[string]any, key string) []string {
	if record == nil {
		return nil
	}
	values, _ := record[key].([]any)
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		text, _ := item.(string)
		text = strings.TrimSpace(text)
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func mustJSONString(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(body)
}
