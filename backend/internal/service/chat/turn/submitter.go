package turn

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	preprocesssvc "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
)

type Store interface {
	CreatePendingTurn(context.Context, common.CreatePendingInput) (common.Conversation, common.Message, error)
}

type PendingRegistrar interface {
	Add(*PendingTurn)
	GetByConversationID(conversationID string) (*PendingTurn, bool)
}

type RealtimePublisher interface {
	PublishConversationUpsert(common.Conversation, []common.Message)
	PublishTimelineItemAppend(string, common.Conversation, timelinesvc.Item)
}

type PreparedImageCleaner interface {
	DeletePreparedImage(context.Context, string) error
}

type SubmitHooks struct {
	AfterCreate   func(ctx context.Context, request protocol.TurnRequest, conversationID string, responseID string)
	NotifyWaiting func(ctx context.Context, ownerID string, title string, userText string)
}

type Submitter struct {
	Store              Store
	Pending            PendingRegistrar
	Realtime           RealtimePublisher
	Hooks              SubmitHooks
	PreparedImageClean PreparedImageCleaner
}

func (s *Submitter) Submit(ctx context.Context, input SubmitInput) (*PendingTurn, common.Conversation, common.Message, error) {
	requestID := "req_" + uuid.NewString()
	responseID := "resp_" + uuid.NewString()
	conversationID := input.Target.ConversationID
	if conversationID == "" {
		conversationID = "conv_" + uuid.NewString()
	}

	conversation, message, err := s.Store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID:    conversationID,
		RequestID:         requestID,
		ResponseID:        responseID,
		OwnerID:           input.OwnerID,
		ReuseConversation: input.Target.Reuse,
		RequestFormat:     input.Request.Protocol.String(),
		Model:             input.Request.Model,
		SystemContent:     input.Request.SystemContent,
		DeveloperContent:  input.Request.DeveloperContent,
		AssistantContent:  input.Request.AssistantContent,
		UserContent:       input.Request.UserContent,
		InputParts:        toStoreInputParts(input.Request.InputParts),
		RequestMethod:     input.RequestMeta.RequestMethod,
		RequestPath:       input.RequestMeta.RequestPath,
		RequestQuery:      input.RequestMeta.RequestQuery,
		RequestHeaders:    input.RequestMeta.RequestHeaders,
		RequestBody:       input.RawBody,
		ToolSchemas:       protocol.RawToolSchemas(input.Request.ToolSchemas),
		ToolChoice:        common.RequestToolChoice{Type: input.Request.ToolChoice.Type, Name: input.Request.ToolChoice.Name},
		ResponseFormat: common.RequestResponseFormat{
			Type:   input.Request.ResponseFormat.Type,
			Name:   input.Request.ResponseFormat.Name,
			Schema: input.Request.ResponseFormat.Schema,
		},
		PreparedImages: toCreatePendingImageAssets(input.PreparedImages),
	})
	if err != nil {
		s.cleanupPreparedImages(ctx, input.PreparedImages)
		return nil, common.Conversation{}, common.Message{}, err
	}

	turn := &PendingTurn{
		RequestID:         requestID,
		ConversationID:    conversationID,
		ResponseID:        responseID,
		OwnerID:           input.OwnerID,
		ToolCallIDs:       extractSubmitToolCallIDs(input.RawBody),
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
	s.Realtime.PublishConversationUpsert(conversation, []common.Message{message})
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

func extractSubmitToolCallIDs(body map[string]any) []string {
	seen := map[string]struct{}{}
	var ids []string
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for _, key := range []string{"tool_call_id", "call_id", "tool_use_id"} {
				if id := strings.TrimSpace(rawStringValue(typed[key], "")); id != "" {
					if _, ok := seen[id]; !ok {
						seen[id] = struct{}{}
						ids = append(ids, id)
					}
				}
			}
			for _, item := range typed {
				visit(item)
			}
		case []any:
			for _, item := range typed {
				visit(item)
			}
		}
	}
	visit(body)
	return ids
}

func rawStringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok {
		return raw
	}
	return fallback
}

func toCreatePendingImageAssets(images []preprocesssvc.PreparedImage) []common.CreatePendingImageAssetInput {
	if len(images) == 0 {
		return nil
	}
	items := make([]common.CreatePendingImageAssetInput, 0, len(images))
	for _, image := range images {
		items = append(items, common.CreatePendingImageAssetInput{
			FileID:            image.FileID,
			Path:              image.Path,
			MediaType:         image.MediaType,
			Bytes:             image.Bytes,
			SHA256:            image.SHA256,
			Width:             image.Width,
			Height:            image.Height,
			SourceKind:        image.SourceKind,
			OriginalName:      image.OriginalName,
			OriginalMediaType: image.OriginalMediaType,
			InputPartIndex:    image.InputPartIndex,
		})
	}
	return items
}

func (s *Submitter) cleanupPreparedImages(ctx context.Context, images []preprocesssvc.PreparedImage) {
	if s.PreparedImageClean == nil {
		return
	}
	for _, image := range images {
		_ = s.PreparedImageClean.DeletePreparedImage(ctx, image.Path)
	}
}

func toStoreInputParts(parts []protocol.InputPart) []common.RequestInputPart {
	if len(parts) == 0 {
		return nil
	}
	items := make([]common.RequestInputPart, 0, len(parts))
	for _, item := range parts {
		items = append(items, common.RequestInputPart{
			Type:      item.Type,
			Text:      item.Text,
			MediaType: item.MediaType,
			URL:       item.URL,
		})
	}
	return items
}
