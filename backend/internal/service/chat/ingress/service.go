package ingress

import (
	"context"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

type Turner interface {
	CreatePendingResponse(context.Context, turnsvc.SubmitInput) (map[string]any, error)
	CreatePendingStream(context.Context, turnsvc.SubmitInput) (*turnsvc.PendingTurn, common.Conversation, error)
}

type Service struct {
	turn Turner
}

type ParsedRequest struct {
	Format      string
	Request     protocol.TurnRequest
	RequestMeta common.Request
}

func New(turn Turner) *Service {
	return &Service{turn: turn}
}

func (s *Service) Parse(_ context.Context, requestFormat string, rawBody map[string]any, requestMeta common.Request) (ParsedRequest, error) {
	parsed, err := protocol.NormalizeRequest(requestFormat, rawBody)
	if err != nil {
		return ParsedRequest{}, err
	}
	return ParsedRequest{
		Format:      requestFormat,
		Request:     parsed,
		RequestMeta: requestMeta,
	}, nil
}

func (s *Service) BuildSubmitInput(parsed ParsedRequest) turnsvc.SubmitInput {
	return turnsvc.SubmitInput{
		Request:     parsed.Request,
		RequestMeta: parsed.RequestMeta,
	}
}

func (s *Service) SubmitResponse(ctx context.Context, parsed ParsedRequest) (map[string]any, error) {
	return s.turn.CreatePendingResponse(ctx, s.BuildSubmitInput(parsed))
}

func (s *Service) SubmitStream(ctx context.Context, parsed ParsedRequest) (*turnsvc.PendingTurn, common.Conversation, error) {
	return s.turn.CreatePendingStream(ctx, s.BuildSubmitInput(parsed))
}
