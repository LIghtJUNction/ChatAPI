package turn

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/protocol"
	"github.com/zyf/chatapi/internal/store"
)

type Store interface {
	CreatePendingTurn(context.Context, store.CreatePendingInput) (store.Conversation, store.Message, error)
}

type PendingRegistrar interface {
	Add(*PendingTurn)
	GetByConversationID(conversationID string) (*PendingTurn, bool)
}

type RealtimePublisher interface {
	PublishConversationUpsert(store.Conversation, []store.Message)
}

type SubmitHooks struct {
	AfterCreate   func(ctx context.Context, request protocol.TurnRequest, conversationID string, responseID string)
	NotifyWaiting func(ctx context.Context, ownerID string, title string, userText string)
}

type Submitter struct {
	Store    Store
	Pending  PendingRegistrar
	Realtime RealtimePublisher
	Hooks    SubmitHooks
}

func (s *Submitter) Submit(ctx context.Context, input SubmitInput) (*PendingTurn, store.Conversation, store.Message, error) {
	requestID := "req_" + uuid.NewString()
	responseID := "resp_" + uuid.NewString()
	conversationID := "conv_" + uuid.NewString()

	conversation, message, err := s.Store.CreatePendingTurn(ctx, store.CreatePendingInput{
		ConversationID:   conversationID,
		RequestID:        requestID,
		ResponseID:       responseID,
		OwnerID:          input.OwnerID,
		RequestFormat:    input.Request.Protocol.String(),
		Model:            input.Request.Model,
		SystemContent:    input.Request.SystemContent,
		DeveloperContent: input.Request.DeveloperContent,
		AssistantContent: input.Request.AssistantContent,
		UserContent:      input.Request.UserContent,
		InputParts:       toStoreInputParts(input.Request.InputParts),
		RequestMethod:    input.RequestMeta.RequestMethod,
		RequestPath:      input.RequestMeta.RequestPath,
		RequestQuery:     input.RequestMeta.RequestQuery,
		RequestHeaders:   input.RequestMeta.RequestHeaders,
		RequestBody:      input.RawBody,
		ToolSchemas:      protocol.RawToolSchemas(input.Request.ToolSchemas),
		ToolChoice:       store.RequestToolChoice{Type: input.Request.ToolChoice.Type, Name: input.Request.ToolChoice.Name},
		ResponseFormat: store.RequestResponseFormat{
			Type:   input.Request.ResponseFormat.Type,
			Name:   input.Request.ResponseFormat.Name,
			Schema: input.Request.ResponseFormat.Schema,
		},
	})
	if err != nil {
		return nil, store.Conversation{}, store.Message{}, err
	}

	turn := &PendingTurn{
		RequestID:         requestID,
		ConversationID:    conversationID,
		ResponseID:        responseID,
		OwnerID:           input.OwnerID,
		Actor:             input.Actor,
		RequestFormat:     input.Request.Protocol.String(),
		Model:             input.Request.Model,
		NormalizedRequest: input.Request,
		RequestMeta:       input.RequestMeta,
		CreatedAt:         time.Now().UTC(),
		Events:            make(chan PendingEvent, 32),
		Done:              make(chan PendingResult, 1),
	}
	s.Pending.Add(turn)
	s.Realtime.PublishConversationUpsert(conversation, []store.Message{message})
	if s.Hooks.AfterCreate != nil {
		s.Hooks.AfterCreate(ctx, input.Request, conversationID, responseID)
	}
	if s.Hooks.NotifyWaiting != nil {
		if _, waiting := s.Pending.GetByConversationID(conversationID); waiting {
			s.Hooks.NotifyWaiting(ctx, input.OwnerID, conversation.Title, input.Request.UserContent)
		}
	}
	return turn, conversation, message, nil
}

func toStoreInputParts(parts []protocol.InputPart) []store.RequestInputPart {
	if len(parts) == 0 {
		return nil
	}
	items := make([]store.RequestInputPart, 0, len(parts))
	for _, item := range parts {
		items = append(items, store.RequestInputPart{
			Type:      item.Type,
			Text:      item.Text,
			MediaType: item.MediaType,
			URL:       item.URL,
		})
	}
	return items
}
