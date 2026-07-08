package ingress

import (
	"context"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	preprocesssvc "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

type Preprocessor interface {
	Prepare(context.Context, string, protocol.TurnRequest) (preprocesssvc.PreparedRequest, error)
}

type Turner interface {
	CreatePendingResponse(context.Context, turnsvc.SubmitInput) (map[string]any, error)
	CreatePendingStream(context.Context, turnsvc.SubmitInput) (*turnsvc.PendingTurn, common.Conversation, error)
}

type Service struct {
	preprocess Preprocessor
	turn       Turner
}

type ParsedRequest struct {
	Format      string
	RawBody     map[string]any
	Request     protocol.TurnRequest
	RequestMeta common.Request
	Prepared    preprocesssvc.PreparedRequest
}

func New(preprocess Preprocessor, turn Turner) *Service {
	return &Service{preprocess: preprocess, turn: turn}
}

func (s *Service) Parse(ctx context.Context, ownerID string, requestFormat string, rawBody map[string]any, requestMeta common.Request) (ParsedRequest, error) {
	parsed, err := protocol.NormalizeRequest(requestFormat, rawBody)
	if err != nil {
		return ParsedRequest{}, err
	}
	prepared := preprocesssvc.PreparedRequest{Request: parsed}
	if s != nil && s.preprocess != nil {
		prepared, err = s.preprocess.Prepare(ctx, ownerID, parsed)
		if err != nil {
			return ParsedRequest{}, err
		}
		parsed = prepared.Request
	}
	return ParsedRequest{
		Format:      requestFormat,
		RawBody:     rawBody,
		Request:     parsed,
		RequestMeta: requestMeta,
		Prepared:    prepared,
	}, nil
}

func (s *Service) BuildSubmitInput(parsed ParsedRequest) turnsvc.SubmitInput {
	return turnsvc.SubmitInput{
		Request:        parsed.Request,
		PreparedImages: parsed.Prepared.PreparedImages,
		RequestMeta:    parsed.RequestMeta,
		RawBody:        parsed.RawBody,
	}
}

func (s *Service) SubmitResponse(ctx context.Context, parsed ParsedRequest) (map[string]any, error) {
	return s.turn.CreatePendingResponse(ctx, s.BuildSubmitInput(parsed))
}

func (s *Service) SubmitStream(ctx context.Context, parsed ParsedRequest) (*turnsvc.PendingTurn, common.Conversation, error) {
	return s.turn.CreatePendingStream(ctx, s.BuildSubmitInput(parsed))
}
