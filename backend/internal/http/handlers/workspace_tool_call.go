package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type WorkspaceToolCallHandler struct {
	Config        config.Config
	Service       *service.WorkspaceToolCallService
	AssistService *service.ToolCallAssistService
	KirariService *service.KirariIntegrationService
	Audit         *service.AuditService
}

func (h WorkspaceToolCallHandler) Schema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"schema": service.BuildWorkspaceToolCallContextSchema(),
	})
}

func (h WorkspaceToolCallHandler) AssistContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := service.RequestActorFromContext(r.Context())
	if !ok || !service.IsInteractiveUserActor(actor) {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	conversationID := strings.TrimSpace(r.URL.Query().Get("conversation_id"))
	contextPayload, err := h.Service.AssistContext(r.Context(), actor.UserID, requestID, conversationID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrToolCallAssistTargetRequired):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, service.ErrForbidden):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                          true,
		"request":                     contextPayload.Request,
		"parsed":                      requestParsedView(requestBaseURL(r), contextPayload.Request),
		"conversation":                contextPayload.Conversation,
		"messages":                    contextPayload.Messages,
		"assist_schema":               h.Service.AssistSchema(),
		"upstream_assistant_schema":   service.BuildUpstreamAssistantSchema(),
		"upstream_protocol_templates": service.BuildUpstreamProtocolTemplates(),
		"upstream_hints":              service.BuildUpstreamAssistantHints(h.Config, requestBaseURL(r), strings.TrimSpace(r.URL.Query().Get("candidate_base_url"))),
		"upstream_input_hints":        service.BuildUpstreamInputHints(contextPayload.Messages, contextPayload.DraftText, 20),
		"draft": map[string]any{
			"text":   contextPayload.DraftText,
			"length": contextPayload.DraftLength,
		},
	})
}

func (h WorkspaceToolCallHandler) Assist(w http.ResponseWriter, r *http.Request) {
	actor, ok := service.RequestActorFromContext(r.Context())
	if !ok || !service.IsInteractiveUserActor(actor) {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	var body struct {
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		RequestID      string `json:"request_id"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	startedAt := time.Now()
	result, err := h.AssistService.Execute(r.Context(), actor.UserID, body.Provider, body.Model, body.RequestID, body.ConversationID)
	if err != nil {
		h.recordAssist(r, actor.UserID, "assist", body.Provider, body.Model, body.RequestID, body.ConversationID, startedAt, nil, err)
		switch {
		case errors.Is(err, service.ErrToolCallAssistProviderRequired),
			errors.Is(err, service.ErrToolCallAssistModelRequired),
			errors.Is(err, service.ErrToolCallAssistTargetRequired),
			errors.Is(err, service.ErrToolCallAssistUnsupported),
			errors.Is(err, service.ErrToolCallAssistNoTools):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, service.ErrKirariNotConnected):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, service.ErrForbidden):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	h.recordAssist(r, actor.UserID, "assist", body.Provider, body.Model, body.RequestID, body.ConversationID, startedAt, &result, nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"assist": result,
	})
}

func (h WorkspaceToolCallHandler) ParseAssistOutput(w http.ResponseWriter, r *http.Request) {
	actor, ok := service.RequestActorFromContext(r.Context())
	if !ok || !service.IsInteractiveUserActor(actor) {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	var body struct {
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		RequestID      string `json:"request_id"`
		ConversationID string `json:"conversation_id"`
		RawOutput      string `json:"raw_output"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	startedAt := time.Now()
	result, err := h.AssistService.Parse(r.Context(), actor.UserID, body.Provider, body.Model, body.RequestID, body.ConversationID, body.RawOutput)
	if err != nil {
		h.recordAssist(r, actor.UserID, "assist_parse", body.Provider, body.Model, body.RequestID, body.ConversationID, startedAt, nil, err)
		switch {
		case errors.Is(err, service.ErrToolCallAssistRawOutputRequired),
			errors.Is(err, service.ErrToolCallAssistTargetRequired),
			errors.Is(err, service.ErrToolCallAssistNoTools):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, service.ErrForbidden):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	h.recordAssist(r, actor.UserID, "assist_parse", body.Provider, body.Model, body.RequestID, body.ConversationID, startedAt, &result, nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"assist": result,
	})
}

func (h WorkspaceToolCallHandler) AssistStream(w http.ResponseWriter, r *http.Request) {
	actor, ok := service.RequestActorFromContext(r.Context())
	if !ok || !service.IsInteractiveUserActor(actor) {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	var body struct {
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		RequestID      string `json:"request_id"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	startedAt := time.Now()
	stream, err := h.AssistService.ExecuteStream(r.Context(), actor.UserID, body.Provider, body.Model, body.RequestID, body.ConversationID)
	if err != nil {
		h.recordAssist(r, actor.UserID, "assist_stream", body.Provider, body.Model, body.RequestID, body.ConversationID, startedAt, nil, err)
		switch {
		case errors.Is(err, service.ErrToolCallAssistProviderRequired),
			errors.Is(err, service.ErrToolCallAssistModelRequired),
			errors.Is(err, service.ErrToolCallAssistTargetRequired),
			errors.Is(err, service.ErrToolCallAssistUnsupported),
			errors.Is(err, service.ErrToolCallAssistNoTools):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, service.ErrKirariNotConnected):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, service.ErrForbidden):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	var completedResult *service.ToolCallAssistResult
	var streamErr error
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-stream.Events:
			if !ok {
				h.recordAssist(r, actor.UserID, "assist_stream", body.Provider, body.Model, body.RequestID, body.ConversationID, startedAt, completedResult, streamErr)
				return
			}
			if event.Event == "assist.completed" {
				if data, _ := event.Data.(map[string]any); data != nil {
					switch assist := data["assist"].(type) {
					case map[string]any:
						completedResult = decodeAssistResultMap(assist)
					case service.ToolCallAssistResult:
						result := assist
						completedResult = &result
					case *service.ToolCallAssistResult:
						completedResult = assist
					}
				}
			}
			if event.Event == "assist.failed" {
				if data, _ := event.Data.(map[string]any); data != nil {
					streamErr = errors.New(stringValue(data["error"], "assist stream failed"))
				} else {
					streamErr = errors.New("assist stream failed")
				}
			}
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h WorkspaceToolCallHandler) recordAssist(
	r *http.Request,
	userID string,
	action string,
	provider string,
	model string,
	requestID string,
	conversationID string,
	startedAt time.Time,
	result *service.ToolCallAssistResult,
	err error,
) {
	if h.Audit == nil {
		return
	}
	resourceID := strings.TrimSpace(requestID)
	if resourceID == "" {
		resourceID = strings.TrimSpace(conversationID)
	}
	metadata := map[string]any{
		"provider":        strings.ToLower(strings.TrimSpace(provider)),
		"model":           strings.TrimSpace(model),
		"request_id":      strings.TrimSpace(requestID),
		"conversation_id": strings.TrimSpace(conversationID),
		"duration_ms":     time.Since(startedAt).Milliseconds(),
	}
	if result != nil {
		metadata["valid_draft"] = result.ValidDraft
		metadata["warning_count"] = len(result.Warnings)
		metadata["validation_error_count"] = len(result.ValidationErrors)
		if result.ToolCall != nil {
			metadata["tool_name"] = stringValue(result.ToolCall["name"], "")
		}
	}
	if strings.EqualFold(strings.TrimSpace(provider), "kirari") && h.KirariService != nil {
		if status, statusErr := h.KirariService.Status(r.Context(), userID); statusErr == nil {
			metadata["issuer_url"] = status.IssuerURL
			metadata["subject"] = status.Subject
		}
	}
	outcome := "success"
	if err != nil {
		outcome = "failure"
		metadata["error"] = err.Error()
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "user.tool_call",
		ResourceType: "workspace_tool_call",
		ResourceID:   resourceID,
		Action:       strings.TrimSpace(action),
		Outcome:      outcome,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata:     metadata,
	})
}

func decodeAssistResultMap(record map[string]any) *service.ToolCallAssistResult {
	if record == nil {
		return nil
	}
	result := &service.ToolCallAssistResult{
		Provider:    stringValue(record["provider"], ""),
		Model:       stringValue(record["model"], ""),
		Explanation: stringValue(record["explanation"], ""),
		Confidence:  stringValue(record["confidence"], ""),
		RawOutput:   stringValue(record["raw_output"], ""),
		ValidDraft:  boolValue(record["valid_draft"], false),
	}
	if request, _ := record["request"].(map[string]any); request != nil {
		result.Request = request
	}
	if toolCall, _ := record["tool_call"].(map[string]any); toolCall != nil {
		result.ToolCall = toolCall
	}
	if warnings, _ := record["warnings"].([]any); len(warnings) > 0 {
		for _, item := range warnings {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result.Warnings = append(result.Warnings, text)
			}
		}
	}
	if validationErrors, _ := record["validation_errors"].([]any); len(validationErrors) > 0 {
		for _, item := range validationErrors {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result.ValidationErrors = append(result.ValidationErrors, text)
			}
		}
	}
	return result
}

func boolValue(value any, fallback bool) bool {
	typed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return typed
}

func requestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	return (&url.URL{Scheme: scheme, Host: host}).String()
}
