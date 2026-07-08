package turn

import (
	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/protocol"
	"github.com/zyf/chatapi/internal/repository/common"
	preprocesssvc "github.com/zyf/chatapi/internal/service/chat/preprocess"
)

type SubmitInput struct {
	OwnerID        string
	Actor          actor.Actor
	Request        protocol.TurnRequest
	PreparedImages []preprocesssvc.PreparedImage
	RequestMeta    common.Request
	RawBody        map[string]any
}
