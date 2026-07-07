package turn

import (
	"time"

	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/protocol"
	"github.com/zyf/chatapi/internal/store"
)

type PendingResult struct {
	ResponseBody map[string]any
}

type PendingEvent struct {
	Type         string
	DeltaText    string
	OutputText   string
	Mode         string
	ToolName     string
	ToolCallID   string
	ToolOutput   string
	ResponseBody map[string]any
	ErrorBody    map[string]any
}

type PendingTurn struct {
	RequestID         string
	ConversationID    string
	ResponseID        string
	OwnerID           string
	Actor             actor.Actor
	RequestFormat     string
	Model             string
	NormalizedRequest protocol.TurnRequest
	RequestMeta       store.Request
	CreatedAt         time.Time
	State             string
	Events            chan PendingEvent
	Done              chan PendingResult
}
