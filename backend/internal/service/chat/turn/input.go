package turn

import (
	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/protocol"
	preprocesssvc "github.com/zyf/chatapi/internal/service/chat/preprocess"
	"github.com/zyf/chatapi/internal/store"
)

type SubmitInput struct {
	OwnerID        string
	Actor          actor.Actor
	Request        protocol.TurnRequest
	PreparedImages []preprocesssvc.PreparedImage
	RequestMeta    store.Request
	RawBody        map[string]any
}
