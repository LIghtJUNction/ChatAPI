package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	egresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/egress"
	ingresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/ingress"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

func TestProtocolRequestMapsSubmissionErrorSemantics(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "invalid input", err: protocol.InvalidRequest("bad image", "input"), wantStatus: http.StatusBadRequest},
		{name: "processor unavailable", err: media.ErrProcessorUnavailable, wantStatus: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := ChatAPIHandler{
				Ingress: ingresssvc.New(errorTurner{err: test.err}),
				Egress:  egresssvc.New(),
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"demo","input":"hello"}`))
			response := httptest.NewRecorder()
			handler.Responses(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("unexpected status: got %d want %d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

type errorTurner struct{ err error }

func (t errorTurner) CreatePendingResponse(context.Context, turnsvc.SubmitInput) (map[string]any, error) {
	return nil, t.err
}

func (t errorTurner) CreatePendingStream(context.Context, turnsvc.SubmitInput) (*turnsvc.PendingTurn, common.Conversation, error) {
	return nil, common.Conversation{}, t.err
}

var _ ingresssvc.Turner = errorTurner{}
