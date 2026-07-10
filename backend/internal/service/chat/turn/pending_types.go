package turn

import (
	"sync"
	"time"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/service/chat/outputpolicy"
	protocolruntime "github.com/zyf2007/ChatAPI/internal/service/chat/protocolruntime"
)

type PendingResult struct {
	ResponseBody map[string]any
}

type PendingEvent struct {
	Action       OutputAction
	ResponseBody map[string]any
	ErrorBody    map[string]any
	StreamEvents []protocol.StreamEvent
}

type PendingTurn struct {
	RequestID         string
	ConversationID    string
	ResponseID        string
	OwnerID           string
	ToolCallIDs       []string
	Actor             actor.Actor
	RequestFormat     string
	Model             string
	NormalizedRequest protocol.TurnRequest
	RequestMeta       common.Request
	Runtime           *protocolruntime.Runtime
	OutputGuard       *outputpolicy.Guard
	MutationMu        *sync.Mutex
	CreatedAt         time.Time
	State             string
	Events            chan PendingEvent
	Done              chan PendingResult
}

func (p *PendingTurn) GetConversationID() string {
	if p == nil {
		return ""
	}
	return p.ConversationID
}
