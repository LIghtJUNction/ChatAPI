package turn

import (
	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/protocol"
	"github.com/zyf/chatapi/internal/store"
)

type SubmitInput struct {
	OwnerID     string
	Actor       actor.Actor
	Request     protocol.TurnRequest
	RequestMeta store.Request
	RawBody     map[string]any
}
