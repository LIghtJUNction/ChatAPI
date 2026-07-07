package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/zyf/chatapi/internal/protocol"
)

var (
	ErrToolCallAssistProviderRequired  = errors.New("assist provider is required")
	ErrToolCallAssistModelRequired     = errors.New("assist model is required")
	ErrToolCallAssistRawOutputRequired = errors.New("assist raw_output is required")
	ErrToolCallAssistUnsupported       = errors.New("assist provider is not supported")
	ErrToolCallAssistNoTools           = errors.New("assist target does not declare any tools")
	ErrToolCallAssistInvalidOutput     = errors.New("assist output is invalid")
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

type ToolCallAssistStream struct {
	Provider string
	Model    string
	Request  map[string]any
	Events   <-chan protocol.StreamEvent
}

type ToolCallAssistService struct {
	workspace *WorkspaceToolCallService
	providers map[string]UpstreamProvider
}

func NewToolCallAssistService(workspace *WorkspaceToolCallService, providers ...UpstreamProvider) *ToolCallAssistService {
	service := &ToolCallAssistService{
		workspace: workspace,
		providers: make(map[string]UpstreamProvider, len(providers)),
	}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		name := normalizeProviderName(provider.ProviderName())
		if name == "" {
			continue
		}
		service.providers[name] = provider
	}
	return service
}

func (s *ToolCallAssistService) Providers() []UpstreamProviderDescriptor {
	if s == nil || len(s.providers) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]UpstreamProviderDescriptor, 0, len(names))
	for _, name := range names {
		provider := s.providers[name]
		if provider == nil {
			continue
		}
		desc := provider.ProviderDescriptor()
		desc.Name = normalizeProviderName(firstNonEmptyStrings(desc.Name, provider.ProviderName(), name))
		if desc.DisplayName == "" {
			desc.DisplayName = desc.Name
		}
		out = append(out, desc)
	}
	return out
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
	if _, ok := s.providers[provider]; !ok {
		return ToolCallAssistResult{}, ErrToolCallAssistUnsupported
	}
	return s.executeProvider(ctx, userID, provider, model, contextPayload)
}

func (s *ToolCallAssistService) ExecuteStream(ctx context.Context, userID string, provider string, model string, requestID string, conversationID string) (*ToolCallAssistStream, error) {
	if s == nil || s.workspace == nil {
		return nil, ErrForbidden
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)
	if provider == "" {
		return nil, ErrToolCallAssistProviderRequired
	}
	if model == "" {
		return nil, ErrToolCallAssistModelRequired
	}
	contextPayload, err := s.workspace.AssistContext(ctx, userID, requestID, conversationID)
	if err != nil {
		return nil, err
	}
	if _, ok := s.providers[provider]; !ok {
		return nil, ErrToolCallAssistUnsupported
	}
	return s.executeProviderStream(ctx, userID, provider, model, contextPayload)
}

func (s *ToolCallAssistService) Parse(ctx context.Context, userID string, provider string, model string, requestID string, conversationID string, rawOutput string) (ToolCallAssistResult, error) {
	if s == nil || s.workspace == nil {
		return ToolCallAssistResult{}, ErrForbidden
	}
	rawOutput = strings.TrimSpace(rawOutput)
	if rawOutput == "" {
		return ToolCallAssistResult{}, ErrToolCallAssistRawOutputRequired
	}
	contextPayload, err := s.workspace.AssistContext(ctx, userID, requestID, conversationID)
	if err != nil {
		return ToolCallAssistResult{}, err
	}
	normalizedTools := protocol.NormalizeToolSchemas(contextPayload.Request.ToolSchemas)
	if len(normalizedTools) == 0 {
		return ToolCallAssistResult{}, ErrToolCallAssistNoTools
	}
	requestMeta := assistRequestMeta(contextPayload)
	return finalizeAssistResult(normalizeProviderName(provider), strings.TrimSpace(model), requestMeta, rawOutput, normalizedTools), nil
}

func (s *ToolCallAssistService) executeProvider(ctx context.Context, userID string, providerName string, model string, contextPayload ToolCallAssistContext) (ToolCallAssistResult, error) {
	provider, ok := s.providers[providerName]
	if !ok || provider == nil {
		return ToolCallAssistResult{}, ErrToolCallAssistUnsupported
	}
	normalizedTools := protocol.NormalizeToolSchemas(contextPayload.Request.ToolSchemas)
	if len(normalizedTools) == 0 {
		return ToolCallAssistResult{}, ErrToolCallAssistNoTools
	}
	requestMeta := assistRequestMeta(contextPayload)
	body := buildAssistChatCompletionsBody(model, false, contextPayload, normalizedTools, s.workspace.AssistSchema())
	payload, err := provider.ChatCompletions(ctx, userID, body)
	if err != nil {
		return ToolCallAssistResult{}, NormalizeToolCallAssistProviderError(providerName, err)
	}
	return finalizeAssistResult(providerName, model, requestMeta, firstChatCompletionContent(payload), normalizedTools), nil
}

const providerKirari = "kirari"

func (s *ToolCallAssistService) executeProviderStream(ctx context.Context, userID string, providerName string, model string, contextPayload ToolCallAssistContext) (*ToolCallAssistStream, error) {
	providerRaw, ok := s.providers[providerName]
	if !ok || providerRaw == nil {
		return nil, ErrToolCallAssistUnsupported
	}
	streamingProvider, ok := providerRaw.(UpstreamStreamingProvider)
	if !ok {
		return nil, ErrToolCallAssistUnsupported
	}
	normalizedTools := protocol.NormalizeToolSchemas(contextPayload.Request.ToolSchemas)
	if len(normalizedTools) == 0 {
		return nil, ErrToolCallAssistNoTools
	}
	requestMeta := assistRequestMeta(contextPayload)
	body := buildAssistChatCompletionsBody(model, true, contextPayload, normalizedTools, s.workspace.AssistSchema())
	resp, err := streamingProvider.ChatCompletionsRaw(ctx, userID, body)
	if err != nil {
		return nil, NormalizeToolCallAssistProviderError(providerName, err)
	}
	events := make(chan protocol.StreamEvent, 32)
	go streamAssistChatCompletionResponse(resp, providerName, model, requestMeta, normalizedTools, events)
	return &ToolCallAssistStream{
		Provider: providerName,
		Model:    model,
		Request:  requestMeta,
		Events:   events,
	}, nil
}

func buildAssistChatCompletionsBody(model string, stream bool, contextPayload ToolCallAssistContext, tools []protocol.NormalizedToolSchema, schema ToolCallAssistSchema) map[string]any {
	return map[string]any{
		"model":    model,
		"stream":   stream,
		"messages": buildAssistMessages(contextPayload, tools, schema),
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "tool_call_assist",
				"schema": schema.OutputJSONSchema,
			},
		},
	}
}

func assistRequestMeta(contextPayload ToolCallAssistContext) map[string]any {
	return map[string]any{
		"request_id":      contextPayload.Request.RequestID,
		"conversation_id": contextPayload.Request.ConversationID,
	}
}

func finalizeAssistResult(provider string, model string, requestMeta map[string]any, content string, normalizedTools []protocol.NormalizedToolSchema) ToolCallAssistResult {
	result := ToolCallAssistResult{
		Provider: provider,
		Model:    model,
		Request:  cloneAnyMap(requestMeta),
	}
	content = strings.TrimSpace(content)
	result.RawOutput = content
	if content == "" {
		result.ValidationErrors = []string{"assistant output is empty"}
		return result
	}
	parsed, err := decodeAssistOutput(content)
	if err != nil {
		result.Explanation = content
		result.ValidationErrors = []string{err.Error()}
		return result
	}
	result.Explanation = stringValueFromMap(parsed, "explanation")
	result.ToolCall = objectValueFromMap(parsed, "tool_call")
	result.Confidence = stringValueFromMap(parsed, "confidence")
	result.Warnings = stringArrayValueFromMap(parsed, "warnings")
	result.ValidationErrors = validateAssistDraft(result.ToolCall, normalizedTools)
	result.ValidDraft = len(result.ValidationErrors) == 0
	return result
}

func streamAssistChatCompletionResponse(resp *http.Response, provider string, model string, requestMeta map[string]any, normalizedTools []protocol.NormalizedToolSchema, events chan<- protocol.StreamEvent) {
	defer close(events)
	if resp == nil {
		events <- protocol.StreamEvent{Event: "assist.failed", Data: assistErrorEventData(&ToolCallAssistProviderError{
			Provider:   provider,
			Code:       "upstream_nil_response",
			Message:    "upstream response is nil",
			HTTPStatus: http.StatusBadGateway,
			Retryable:  true,
		}), Done: true}
		return
	}
	defer resp.Body.Close()
	events <- protocol.StreamEvent{
		Event: "assist.started",
		Data: map[string]any{
			"provider": provider,
			"model":    model,
			"request":  cloneAnyMap(requestMeta),
		},
	}
	reader := bufio.NewReader(resp.Body)
	var dataLines []string
	var rawOutput strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			events <- protocol.StreamEvent{Event: "assist.failed", Data: assistErrorEventData(&ToolCallAssistProviderError{
				Provider:   provider,
				Code:       "upstream_stream_read_failed",
				Message:    err.Error(),
				HTTPStatus: http.StatusBadGateway,
				Retryable:  true,
			}), Done: true}
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(dataLines) > 0 {
				chunk := strings.Join(dataLines, "\n")
				dataLines = dataLines[:0]
				if chunk != "[DONE]" {
					if delta := extractAssistChatCompletionDelta(chunk); delta != "" {
						rawOutput.WriteString(delta)
						events <- protocol.StreamEvent{
							Event: "assist.delta",
							Data: map[string]any{
								"delta":      delta,
								"raw_output": rawOutput.String(),
							},
						}
					}
				}
			}
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	result := finalizeAssistResult(provider, model, requestMeta, rawOutput.String(), normalizedTools)
	events <- protocol.StreamEvent{
		Event: "assist.completed",
		Data:  map[string]any{"assist": result},
		Done:  true,
	}
}

func assistErrorEventData(err *ToolCallAssistProviderError) map[string]any {
	if err == nil {
		return map[string]any{"error": "assist stream failed"}
	}
	return map[string]any{
		"error":       err.Message,
		"error_code":  err.Code,
		"provider":    err.Provider,
		"retryable":   err.Retryable,
		"http_status": err.HTTPStatus,
	}
}

func extractAssistChatCompletionDelta(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	firstChoice, _ := choices[0].(map[string]any)
	if delta, _ := firstChoice["delta"].(map[string]any); delta != nil {
		switch content := delta["content"].(type) {
		case string:
			return content
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
			return strings.Join(parts, "")
		}
	}
	if message, _ := firstChoice["message"].(map[string]any); message != nil {
		return firstChatCompletionContent(map[string]any{"choices": []any{map[string]any{"message": message}}})
	}
	return ""
}

func buildAssistMessages(contextPayload ToolCallAssistContext, tools []protocol.NormalizedToolSchema, schema ToolCallAssistSchema) []map[string]any {
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
	if fenced := extractFencedJSONObject(raw); fenced != "" {
		if parsed, err := decodeJSONObject(fenced); err == nil {
			return parsed, nil
		}
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

func extractFencedJSONObject(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	var builder strings.Builder
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				break
			}
			inFence = true
			continue
		}
		if inFence {
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
	}
	return strings.TrimSpace(builder.String())
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
