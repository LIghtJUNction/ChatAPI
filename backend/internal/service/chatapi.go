package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/protocol"
	"github.com/zyf/chatapi/internal/store"
)

var ErrStorageConversationQuotaExceeded = errors.New("storage quota exceeded for new conversations")

type ChatAPIService struct {
	cfg      config.Config
	store    store.Store
	pending  *PendingRegistry
	realtime *RealtimeHub
	auto     *AutomationRuleService
	autoObs  *AutomationObserver
}

type ExpirePendingTurnsResult struct {
	ExpiredConversations int `json:"expired_conversations"`
	ExpiredActiveTurns   int `json:"expired_active_turns"`
}

func NewChatAPIService(cfg config.Config, dataStore store.Store, pending *PendingRegistry, realtime *RealtimeHub) *ChatAPIService {
	return &ChatAPIService{
		cfg:      cfg,
		store:    dataStore,
		pending:  pending,
		realtime: realtime,
		auto:     NewAutomationRuleService(dataStore),
		autoObs:  NewAutomationObserver(),
	}
}

func (s *ChatAPIService) AutomationObserver() *AutomationObserver {
	if s == nil {
		return nil
	}
	return s.autoObs
}

func (s *ChatAPIService) CreatePendingResponse(ctx context.Context, request protocol.TurnRequest, body map[string]any, requestMeta store.Request) (map[string]any, error) {
	turn, _, _, err := s.createPendingTurn(ctx, request, body, requestMeta)
	if err != nil {
		return nil, err
	}
	result, err := s.pending.WaitTurn(ctx, turn)
	if err != nil {
		return nil, err
	}
	return result.ResponseBody, nil
}

func (s *ChatAPIService) CreatePendingStream(ctx context.Context, request protocol.TurnRequest, body map[string]any, requestMeta store.Request) (*PendingTurn, store.Conversation, error) {
	turn, conversation, _, err := s.createPendingTurn(ctx, request, body, requestMeta)
	if err != nil {
		return nil, store.Conversation{}, err
	}
	return turn, conversation, nil
}

func (s *ChatAPIService) createPendingTurn(ctx context.Context, parsed protocol.TurnRequest, body map[string]any, requestMeta store.Request) (*PendingTurn, store.Conversation, store.Message, error) {
	ownerID := OwnerIDFromContext(ctx)
	if err := s.ensureConversationAdmission(ctx, ownerID); err != nil {
		return nil, store.Conversation{}, store.Message{}, err
	}
	requestID := "req_" + uuid.NewString()
	responseID := "resp_" + uuid.NewString()
	conversationID := "conv_" + uuid.NewString()

	conversation, message, err := s.store.CreatePendingTurn(ctx, store.CreatePendingInput{
		ConversationID:   conversationID,
		RequestID:        requestID,
		ResponseID:       responseID,
		OwnerID:          ownerID,
		RequestFormat:    parsed.Protocol.String(),
		Model:            parsed.Model,
		SystemContent:    parsed.SystemContent,
		DeveloperContent: parsed.DeveloperContent,
		AssistantContent: parsed.AssistantContent,
		UserContent:      parsed.UserContent,
		InputParts:       toStoreInputParts(parsed.InputParts),
		RequestMethod:    requestMeta.RequestMethod,
		RequestPath:      requestMeta.RequestPath,
		RequestQuery:     requestMeta.RequestQuery,
		RequestHeaders:   requestMeta.RequestHeaders,
		RequestBody:      body,
		ToolSchemas:      parsed.ToolSchemas,
		ToolChoice:       store.RequestToolChoice{Type: parsed.ToolChoice.Type, Name: parsed.ToolChoice.Name},
		ResponseFormat: store.RequestResponseFormat{
			Type:   parsed.ResponseFormat.Type,
			Name:   parsed.ResponseFormat.Name,
			Schema: parsed.ResponseFormat.Schema,
		},
	})
	if err != nil {
		return nil, store.Conversation{}, store.Message{}, err
	}

	turn := &PendingTurn{
		RequestID:      requestID,
		ConversationID: conversationID,
		ResponseID:     responseID,
		RequestFormat:  parsed.Protocol.String(),
		Model:          parsed.Model,
		CreatedAt:      time.Now().UTC(),
		Events:         make(chan PendingEvent, 32),
		done:           make(chan PendingResult, 1),
	}
	s.pending.Add(turn)
	s.realtime.PublishConversationUpsert(conversation, []store.Message{message})
	s.tryAutomationComplete(ctx, parsed, conversationID, responseID)
	return turn, conversation, message, nil
}

func (s *ChatAPIService) ensureConversationAdmission(ctx context.Context, ownerID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil
	}
	enabled, err := s.storageBlockNewConversationsEnabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	monitor := NewStorageMonitorService(s.cfg, s.store)
	usage, err := monitor.UserUsage(ctx, ownerID)
	if err != nil {
		return err
	}
	if usage.StorageQuotaBytes <= 0 || !usage.StorageOverQuota {
		return nil
	}
	return ErrStorageConversationQuotaExceeded
}

func (s *ChatAPIService) storageBlockNewConversationsEnabled(ctx context.Context) (bool, error) {
	enabled := s.cfg.StorageBlockNewConversations
	if s == nil || s.store == nil {
		return enabled, nil
	}
	item, err := s.store.GetSystemConfig(ctx, systemSettingsConfigKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return enabled, nil
		}
		return false, err
	}
	if raw, ok := item.Value["storage_block_new_conversations"]; ok {
		enabled = settingsBool(raw, enabled)
	}
	return enabled, nil
}

func (s *ChatAPIService) tryAutomationComplete(ctx context.Context, request protocol.TurnRequest, conversationID string, responseID string) {
	if s == nil || s.auto == nil {
		return
	}
	ownerID := OwnerIDFromContext(ctx)
	audit := NewAuditService(s.store)
	decision, err := s.auto.MatchTurn(ctx, ownerID, request, conversationID, responseID)
	if err != nil {
		audit.Record(ctx, AuditEventInput{
			EventType:    "automation.rule",
			ResourceType: "automation_rule",
			Action:       "auto_complete",
			Outcome:      "failure",
			Metadata: map[string]any{
				"conversation_id": conversationID,
				"request_format":  request.Protocol.String(),
				"model":           request.Model,
				"reason":          "list_rules_failed",
				"error":           err.Error(),
			},
		})
		return
	}
	switch decision.Status {
	case automationStatusNoRules:
		s.autoObs.RecordNoRules()
		audit.Record(ctx, AuditEventInput{
			EventType:    "automation.rule",
			ResourceType: "automation_rule",
			Action:       "auto_complete",
			Outcome:      "skipped",
			Metadata: map[string]any{
				"conversation_id": conversationID,
				"request_format":  request.Protocol.String(),
				"model":           request.Model,
				"reason":          automationStatusNoRules,
			},
		})
		return
	case automationStatusNoMatch:
		s.autoObs.RecordNoMatch()
		s.autoObs.RecordSkipReasons(decision.SkipReasons)
		s.autoObs.RecordSkipDetails(decision.SkipDetails)
		s.autoObs.RecordSkipSamples(automationSkipSamples(decision.SkipDetails, request, conversationID))
		audit.Record(ctx, AuditEventInput{
			EventType:    "automation.rule",
			ResourceType: "automation_rule",
			Action:       "auto_complete",
			Outcome:      "skipped",
			Metadata: map[string]any{
				"conversation_id": conversationID,
				"request_format":  request.Protocol.String(),
				"model":           request.Model,
				"reason":          automationStatusNoMatch,
				"skip_count":      len(decision.SkipDetails),
				"skip_reasons":    decision.SkipReasons,
			},
		})
		for _, detail := range decision.SkipDetails {
			audit.Record(ctx, AuditEventInput{
				EventType:    "automation.rule",
				ResourceType: "automation_rule",
				ResourceID:   detail.RuleID,
				Action:       "rule_skip",
				Outcome:      "skipped",
				Metadata: map[string]any{
					"conversation_id": conversationID,
					"request_format":  request.Protocol.String(),
					"model":           request.Model,
					"reason":          detail.Reason,
				},
			})
		}
		return
	}
	if decision.Match == nil {
		return
	}
	if _, err := s.CompleteConversation(ctx, decision.Match.Input); err == nil {
		audit.Record(ctx, AuditEventInput{
			EventType:    "automation.rule",
			ResourceType: "automation_rule",
			ResourceID:   decision.Match.RuleID,
			Action:       "auto_complete",
			Outcome:      "success",
			Metadata: map[string]any{
				"conversation_id": conversationID,
				"request_format":  request.Protocol.String(),
				"model":           request.Model,
			},
		})
		return
	} else {
		audit.Record(ctx, AuditEventInput{
			EventType:    "automation.rule",
			ResourceType: "automation_rule",
			ResourceID:   decision.Match.RuleID,
			Action:       "auto_complete",
			Outcome:      "failure",
			Metadata: map[string]any{
				"conversation_id": conversationID,
				"request_format":  request.Protocol.String(),
				"model":           request.Model,
				"reason":          "complete_pending_failed",
				"error":           err.Error(),
			},
		})
	}
}

func automationSkipSamples(details []AutomationRuleSkipDetail, request protocol.TurnRequest, conversationID string) []AutomationSkipSample {
	if len(details) == 0 {
		return nil
	}
	samples := make([]AutomationSkipSample, 0, len(details))
	for _, detail := range details {
		samples = append(samples, AutomationSkipSample{
			ConversationID: conversationID,
			RequestFormat:  request.Protocol.String(),
			Model:          request.Model,
			RuleID:         detail.RuleID,
			Reason:         detail.Reason,
		})
	}
	return samples
}

func toStoreInputParts(parts []protocol.InputPart) []store.RequestInputPart {
	if len(parts) == 0 {
		return nil
	}
	items := make([]store.RequestInputPart, 0, len(parts))
	for _, part := range parts {
		items = append(items, store.RequestInputPart{
			Type:      part.Type,
			Text:      part.Text,
			MediaType: part.MediaType,
			URL:       part.URL,
		})
	}
	return items
}

func (s *ChatAPIService) ListMessages(ctx context.Context, conversationID string) ([]store.Message, error) {
	return s.store.ListMessages(ctx, conversationID)
}

func (s *ChatAPIService) ListMessagesForOwner(ctx context.Context, conversationID string, ownerID string) ([]store.Message, error) {
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if ownerID != "" && stringValue(conversation.Metadata["owner_id"], "") != ownerID {
		return nil, ErrForbidden
	}
	return s.store.ListMessages(ctx, conversationID)
}

func (s *ChatAPIService) ListConversationsForOwner(ctx context.Context, ownerID string) ([]store.Conversation, error) {
	items, err := s.store.ListConversations(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]store.Conversation, 0, len(items))
	for _, item := range items {
		if ownerID == "" || stringValue(item.Metadata["owner_id"], "") == ownerID {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *ChatAPIService) ListRequests(ctx context.Context) ([]store.Request, error) {
	return s.store.ListRequests(ctx)
}

func (s *ChatAPIService) ListRequestsForOwner(ctx context.Context, ownerID string) ([]store.Request, error) {
	items, err := s.store.ListRequests(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]store.Request, 0, len(items))
	for _, item := range items {
		if ownerID == "" || item.OwnerID == ownerID {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *ChatAPIService) GetRequest(ctx context.Context, requestID string) (store.Request, error) {
	return s.store.GetRequest(ctx, requestID)
}

func (s *ChatAPIService) GetRequestForOwner(ctx context.Context, requestID string, ownerID string) (store.Request, error) {
	item, err := s.store.GetRequest(ctx, requestID)
	if err != nil {
		return store.Request{}, err
	}
	if ownerID != "" && item.OwnerID != ownerID {
		return store.Request{}, ErrForbidden
	}
	return item, nil
}

func (s *ChatAPIService) UpdateDraft(ctx context.Context, conversationID string, chunk string) (map[string]any, error) {
	previousState, err := s.pending.StartDelta(conversationID)
	if err != nil {
		return nil, s.resolveTurnMutationError(ctx, conversationID, err)
	}
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		s.pending.RevertFinalize(conversationID, previousState)
		return nil, err
	}
	metadata := conversation.Metadata
	existing, _ := metadata["realtime_draft_text"].(string)
	nextDraft := existing + chunk
	updated, err := s.store.UpdateDraft(ctx, store.UpdateDraftInput{
		ConversationID: conversationID,
		DraftText:      nextDraft,
	})
	if err != nil {
		s.pending.RevertFinalize(conversationID, previousState)
		return nil, err
	}
	_ = s.pending.Publish(conversationID, PendingEvent{
		Type:      "delta",
		DeltaText: chunk,
	})
	s.realtime.PublishConversationUpsert(updated, nil)
	return map[string]any{
		"draft_text":   nextDraft,
		"draft_length": len([]rune(nextDraft)),
	}, nil
}

func (s *ChatAPIService) CompleteConversation(ctx context.Context, input store.CompletePendingInput) (map[string]any, error) {
	previousState, err := s.pending.StartComplete(input.ConversationID)
	if err != nil {
		return nil, s.resolveTurnMutationError(ctx, input.ConversationID, err)
	}
	conversation, message, err := s.store.CompletePendingTurn(ctx, input)
	if err != nil {
		s.pending.RevertFinalize(input.ConversationID, previousState)
		return nil, err
	}
	messages, err := s.store.ListMessages(ctx, input.ConversationID)
	if err == nil {
		s.realtime.PublishConversationUpsert(conversation, messages)
	} else {
		s.realtime.PublishConversationUpsert(conversation, []store.Message{message})
	}

	responseBody := protocol.BuildResponse(conversation, protocol.TurnResult{
		ResponseID: stringValue(conversation.ResponseID, input.ResponseID),
		OutputText: message.Content,
		Mode:       input.Mode,
		ToolName:   input.ToolName,
		ToolCallID: input.ToolCallID,
		ToolOutput: stringValue(input.ToolOutput, message.Content),
	})
	_ = s.pending.Publish(input.ConversationID, PendingEvent{
		Type:         "complete",
		OutputText:   message.Content,
		Mode:         input.Mode,
		ToolName:     input.ToolName,
		ToolCallID:   input.ToolCallID,
		ToolOutput:   stringValue(input.ToolOutput, message.Content),
		ResponseBody: responseBody,
	})

	if err := s.pending.Resolve(input.ConversationID, PendingResult{ResponseBody: responseBody}); err != nil {
		return nil, err
	}

	return map[string]any{
		"conversation": conversation,
		"output_text":  message.Content,
	}, nil
}

func (s *ChatAPIService) AbortConversation(ctx context.Context, conversationID string, reason string) error {
	previousState, err := s.pending.StartAbort(conversationID)
	if err != nil {
		return s.resolveTurnMutationError(ctx, conversationID, err)
	}
	conversation, message, err := s.store.AbortPendingTurn(ctx, store.AbortPendingInput{
		ConversationID: conversationID,
		Reason:         reason,
	})
	if err != nil {
		s.pending.RevertFinalize(conversationID, previousState)
		return err
	}
	messages, listErr := s.store.ListMessages(ctx, conversationID)
	if listErr == nil {
		s.realtime.PublishConversationUpsert(conversation, messages)
	} else {
		s.realtime.PublishConversationUpsert(conversation, []store.Message{message})
	}

	body := protocol.AbortError(stringValue(conversation.Metadata["request_format"], string(protocol.ProtocolResponses)), reason)
	_ = s.pending.Publish(conversationID, PendingEvent{
		Type:      "abort",
		ErrorBody: body,
	})

	return s.pending.Abort(conversationID, body)
}

func (s *ChatAPIService) ExpirePendingTurns(ctx context.Context, ttl time.Duration, now time.Time) (ExpirePendingTurnsResult, error) {
	if ttl <= 0 {
		return ExpirePendingTurnsResult{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.UTC().Add(-ttl)
	body := pendingExpiredBody(ttl)
	activeExpired := s.pending.ExpireOlderThan(cutoff, body)
	dbResult, err := s.store.ExpirePendingTurns(ctx, cutoff)
	if err != nil {
		return ExpirePendingTurnsResult{}, err
	}
	return ExpirePendingTurnsResult{
		ExpiredConversations: dbResult.ExpiredConversations,
		ExpiredActiveTurns:   activeExpired,
	}, nil
}

func (s *ChatAPIService) resolveTurnMutationError(ctx context.Context, conversationID string, err error) error {
	if !errors.Is(err, ErrPendingNotFound) {
		return err
	}
	conversation, getErr := s.store.GetConversation(ctx, conversationID)
	if getErr != nil {
		return err
	}
	status := stringValue(conversation.Metadata["realtime_status"], "")
	if status == "closed" || status == "aborted" || status == "expired" {
		return ErrPendingConflict
	}
	return err
}

func pendingExpiredBody(ttl time.Duration) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"message": "pending turn expired after " + ttl.String(),
			"type":    "request_timeout",
			"code":    "request_timeout",
		},
	}
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}

func MustConversationID(input map[string]any) (string, error) {
	conversationID, ok := input["conversation_id"].(string)
	if !ok || strings.TrimSpace(conversationID) == "" {
		return "", fmt.Errorf("conversation_id is required")
	}
	return strings.TrimSpace(conversationID), nil
}
