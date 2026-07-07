package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type AppAPIHandler struct {
	Service         *service.ChatAPIService
	ModelAPIKeys    *service.ModelAPIKeyService
	AutomationRules *service.AutomationRuleService
}

func (h AppAPIHandler) MeSchema(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schema": service.BuildAppOverviewSchema()})
}

func (h AppAPIHandler) StatisticsSchema(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schema": service.BuildAppOverviewSchema()})
}

func (h AppAPIHandler) RequestsSchema(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schema": service.BuildAppRequestsSchema()})
}

func (h AppAPIHandler) ConversationsSchema(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schema": service.BuildAppConversationsSchema()})
}

func (h AppAPIHandler) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"app_api_key": map[string]any{
			"id":         principal.KeyID,
			"name":       principal.Name,
			"key_prefix": principal.KeyPrefix,
		},
		"user": map[string]any{
			"id": principal.UserID,
		},
	})
}

func (h AppAPIHandler) ListRequests(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Service.ListRequestsForOwner(r.Context(), principal.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items = filterRequestsForAppAPI(items, principal)
	parsedItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		parsedItems = append(parsedItems, requestParsedSummary(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"items":        items,
		"parsed_items": parsedItems,
	})
}

func (h AppAPIHandler) GetRequest(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	item, err := h.Service.GetRequestForOwner(r.Context(), requestID, principal.UserID)
	if err != nil {
		h.writeRequestError(w, err)
		return
	}
	if !appAPIRequestAllowed(principal, item) {
		h.writeRequestError(w, service.ErrForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "request": item, "parsed": requestParsedView(requestBaseURL(r), item)})
}

func (h AppAPIHandler) CopyRequestCurl(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	item, err := h.Service.GetRequestForOwner(r.Context(), requestID, principal.UserID)
	if err != nil {
		h.writeRequestError(w, err)
		return
	}
	if !appAPIRequestAllowed(principal, item) {
		h.writeRequestError(w, service.ErrForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"request_id": requestID,
		"curl":       buildReplayCurl(requestBaseURL(r), item),
	})
}

func (h AppAPIHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Service.ListConversationsForOwner(r.Context(), principal.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items = filterConversationsForAppAPI(items, principal)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h AppAPIHandler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	items, err := h.Service.ListMessagesForOwner(r.Context(), conversationID, principal.UserID)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !appAPIConversationIDAllowed(principal, conversationID) {
		http.Error(w, service.ErrForbidden.Error(), http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h AppAPIHandler) ListModelAPIKeys(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.ModelAPIKeys.ListKeysForUser(r.Context(), principal.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allowedIDs := stringSetFromAny(principal.ResourceLimits["allowed_model_key_ids"])
	if len(allowedIDs) > 0 {
		filtered := make([]store.ModelAPIKey, 0, len(items))
		for _, item := range items {
			if _, ok := allowedIDs[item.ID]; ok {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h AppAPIHandler) ModelAPIKeySchema(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schema": h.ModelAPIKeys.AppSchema()})
}

func (h AppAPIHandler) CreateModelAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	model := stringValue(body["model"], "")
	allowedModels := stringSetFromAny(principal.ResourceLimits["allowed_virtual_models"])
	if len(allowedModels) > 0 {
		if _, ok := allowedModels[model]; !ok {
			http.Error(w, "app api key forbidden", http.StatusForbidden)
			return
		}
	}
	maxModelKeys := positiveIntFromAny(principal.ResourceLimits["max_model_keys"])
	if maxModelKeys > 0 {
		items, err := h.ModelAPIKeys.ListKeysForUser(r.Context(), principal.UserID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		activeCount := 0
		for _, item := range items {
			if item.RevokedAt == nil {
				activeCount++
			}
		}
		if activeCount >= maxModelKeys {
			http.Error(w, "app api key model key limit exceeded", http.StatusForbidden)
			return
		}
	}
	item, rawKey, err := h.ModelAPIKeys.CreateKey(r.Context(), principal.UserID, stringValue(body["name"], "model key"), model)
	if err != nil {
		if errors.Is(err, service.ErrModelRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"item":    item,
		"raw_key": rawKey,
	})
}

func (h AppAPIHandler) DeleteModelAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	keyID := strings.TrimSpace(chi.URLParam(r, "keyID"))
	if keyID == "" {
		http.Error(w, "key_id is required", http.StatusBadRequest)
		return
	}
	allowedIDs := stringSetFromAny(principal.ResourceLimits["allowed_model_key_ids"])
	if len(allowedIDs) > 0 {
		if _, ok := allowedIDs[keyID]; !ok {
			http.Error(w, "app api key forbidden", http.StatusForbidden)
			return
		}
	}
	if err := h.ModelAPIKeys.RevokeKey(r.Context(), principal.UserID, keyID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h AppAPIHandler) ListAutomationRules(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	rules, err := h.AutomationRules.ListRules(r.Context(), principal.UserID, stringSetFromAny(principal.ResourceLimits["allowed_automation_rule_ids"]))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": rules})
}

func (h AppAPIHandler) AutomationRuleSchema(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schema": h.AutomationRules.AppSchema()})
}

func (h AppAPIHandler) PutAutomationRules(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	rules, err := mapSlice(body["rules"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nextRules, err := h.AutomationRules.ReplaceRules(r.Context(), principal.UserID, stringSetFromAny(principal.ResourceLimits["allowed_automation_rule_ids"]), rules)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, "app api key forbidden", http.StatusForbidden)
			return
		}
		if errors.Is(err, service.ErrInvalidAutomationRule) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": nextRules})
}

func (h AppAPIHandler) StatisticsSummary(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	summary, err := h.Service.StatisticsSummaryForOwner(r.Context(), principal.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "summary": summary})
}

func (h AppAPIHandler) RequestDelta(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, "delta", service.TurnControlStreamDelta)
}

func (h AppAPIHandler) RequestComplete(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, "complete", service.TurnControlStreamComplete)
}

func (h AppAPIHandler) RequestAbort(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, "abort", service.TurnControlAbort)
}

func (h AppAPIHandler) executeRequestTurnControl(w http.ResponseWriter, r *http.Request, action string, kind service.TurnControlKind) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	if len(principal.AllowedActions) > 0 {
		if _, ok := principal.AllowedActions[action]; !ok {
			http.Error(w, "app api key forbidden", http.StatusForbidden)
			return
		}
	}

	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	request, err := h.Service.GetRequestForOwner(r.Context(), requestID, principal.UserID)
	if err != nil {
		h.writeRequestError(w, err)
		return
	}
	if !appAPIRequestAllowed(principal, request) {
		h.writeRequestError(w, service.ErrForbidden)
		return
	}
	body := decodeBodyOrEmpty(r)
	command := service.TurnControlCommand{
		Kind:                kind,
		ResponseID:          stringValue(body["response_id"], ""),
		OutputText:          stringValue(body["text"], ""),
		Mode:                stringValue(body["mode"], "assistant_message"),
		ToolName:            stringValue(body["tool_name"], ""),
		ToolCallID:          stringValue(body["tool_call_id"], ""),
		ToolOutput:          stringValue(body["output"], stringValue(body["text"], "")),
		ReasoningStreamMode: stringValue(body["reasoning_stream_mode"], ""),
		AbortReason:         stringValue(body["error"], ""),
	}
	result, err := h.Service.ExecuteTurnControlByRequestID(r.Context(), requestID, command)
	if err != nil {
		if errors.Is(err, service.ErrPendingConflict) || errors.Is(err, store.ErrTurnConflict) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, service.ErrPendingNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h AppAPIHandler) writeRequestError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, service.ErrPendingNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func stringSetFromAny(value any) map[string]struct{} {
	items := stringSlice(value)
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out[item] = struct{}{}
		}
	}
	return out
}

func filterRequestsForAppAPI(items []store.Request, principal service.AppAPIPrincipal) []store.Request {
	if len(items) == 0 {
		return []store.Request{}
	}
	filtered := make([]store.Request, 0, len(items))
	for _, item := range items {
		if appAPIRequestAllowed(principal, item) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func appAPIRequestAllowed(principal service.AppAPIPrincipal, item store.Request) bool {
	if !appAPIModelAllowed(principal, item.Model) {
		return false
	}
	if !appAPIRequestIDAllowed(principal, item.RequestID) {
		return false
	}
	if !appAPIConversationIDAllowed(principal, item.ConversationID) {
		return false
	}
	return true
}

func filterConversationsForAppAPI(items []store.Conversation, principal service.AppAPIPrincipal) []store.Conversation {
	if len(items) == 0 {
		return []store.Conversation{}
	}
	filtered := make([]store.Conversation, 0, len(items))
	for _, item := range items {
		if appAPIConversationIDAllowed(principal, item.ID) && appAPIModelAllowed(principal, stringValue(item.Metadata["model"], "")) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func appAPIRequestIDAllowed(principal service.AppAPIPrincipal, requestID string) bool {
	allowed := stringSetFromAny(principal.ResourceLimits["allowed_request_ids"])
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[strings.TrimSpace(requestID)]
	return ok
}

func appAPIConversationIDAllowed(principal service.AppAPIPrincipal, conversationID string) bool {
	allowed := stringSetFromAny(principal.ResourceLimits["allowed_conversation_ids"])
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[strings.TrimSpace(conversationID)]
	return ok
}

func appAPIModelAllowed(principal service.AppAPIPrincipal, model string) bool {
	allowed := stringSetFromAny(principal.ResourceLimits["allowed_virtual_models"])
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[strings.TrimSpace(model)]
	return ok
}

func positiveIntFromAny(value any) int {
	switch raw := value.(type) {
	case int:
		if raw > 0 {
			return raw
		}
	case int64:
		if raw > 0 && raw <= int64(maxIntForHandler) {
			return int(raw)
		}
	case float64:
		if raw > 0 && raw <= float64(maxIntForHandler) {
			return int(raw)
		}
	}
	return 0
}

const maxIntForHandler = int(^uint(0) >> 1)

func mapSlice(value any) ([]map[string]any, error) {
	rawItems, ok := value.([]any)
	if !ok {
		return nil, errors.New("rules must be an array")
	}
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("rule must be an object")
		}
		items = append(items, item)
	}
	return items, nil
}
