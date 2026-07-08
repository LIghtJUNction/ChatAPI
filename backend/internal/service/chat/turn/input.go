package turn

import (
	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	preprocesssvc "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess"
)

type SubmitInput struct {
	OwnerID        string
	Actor          actor.Actor
	Request        protocol.TurnRequest
	PreparedImages []preprocesssvc.PreparedImage
	RequestMeta    common.Request
	RawBody        map[string]any
	Target         SubmitTarget
}

type SubmitTarget struct {
	ConversationID string
	Reuse          bool
	Source         string
}
