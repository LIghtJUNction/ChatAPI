package turn

import (
	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type SubmitInput struct {
	OwnerID        string
	Actor          actor.Actor
	Request        protocol.TurnRequest
	RequestBody    map[string]any
	PreparedImages []media.DraftAsset
	RequestMeta    common.Request
	Target         SubmitTarget
}

type SubmitTarget struct {
	ConversationID string
	Reuse          bool
	Source         string
}
