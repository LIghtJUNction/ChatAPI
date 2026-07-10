package workspace

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	automationsvc "github.com/zyf2007/ChatAPI/internal/service/automation"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

type ConversationQuery interface {
	ListConversationsForOwner(context.Context, string) ([]common.Conversation, error)
}

type TimelineQuery interface {
	ListTimelineForOwner(context.Context, string, string) ([]timelinesvc.Item, error)
}

type Snapshot struct {
	Type          string                `json:"type"`
	Conversations []ConversationSummary `json:"conversations"`
}

type ConversationUpsert struct {
	Type         string              `json:"type"`
	Conversation ConversationSummary `json:"conversation"`
}

type ConversationDelete struct {
	Type           string `json:"type"`
	ConversationID string `json:"conversation_id"`
}

type TimelineReset struct {
	Type           string         `json:"type"`
	ConversationID string         `json:"conversation_id"`
	Items          []TimelineItem `json:"items"`
}

type TimelineItemAppend struct {
	Type           string       `json:"type"`
	ConversationID string       `json:"conversation_id"`
	Item           TimelineItem `json:"item"`
}

type ConnectionCount struct {
	Type                   string `json:"type"`
	CurrentConnectionCount int    `json:"current_connection_count"`
}

type ClientMessage struct {
	Type           string   `json:"type"`
	CommandID      string   `json:"command_id,omitempty"`
	ConversationID string   `json:"conversation_id,omitempty"`
	Command        *Command `json:"command,omitempty"`
}

var ErrInvalidClientMessage = errors.New("invalid workspace client message")

type Service struct {
	conversations ConversationQuery
	timeline      TimelineQuery
	turn          TurnExecutor
	automation    AutomationRecorder
}

type TurnExecutor interface {
	Execute(context.Context, controlsvc.Command) (controlsvc.Result, error)
}

type AutomationRecorder interface {
	StartRecording(context.Context, string, string) (automationsvc.RecordingState, error)
	StopRecording(context.Context, string) (automationsvc.RecordingState, error)
	CancelRecording(context.Context, string) (automationsvc.RecordingState, error)
	RecordingState(string) automationsvc.RecordingState
	ExecutionStates(string) []automationsvc.ExecutionState
	StateSnapshot(string) automationsvc.StateSnapshot
}

func New(conversations ConversationQuery, timeline TimelineQuery, turn TurnExecutor, automation ...AutomationRecorder) *Service {
	service := &Service{conversations: conversations, timeline: timeline, turn: turn}
	if len(automation) > 0 {
		service.automation = automation[0]
	}
	return service
}

func (s *Service) SetAutomation(automation AutomationRecorder) { s.automation = automation }

func (s *Service) Snapshot(ctx context.Context, ownerID string) (Snapshot, error) {
	items, err := s.conversations.ListConversationsForOwner(ctx, strings.TrimSpace(ownerID))
	if err != nil {
		return Snapshot{}, err
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	summaries := make([]ConversationSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, SummaryFromConversation(item))
	}
	return Snapshot{Type: "workspace.snapshot", Conversations: summaries}, nil
}

func (s *Service) TimelineReset(ctx context.Context, ownerID string, conversationID string) (TimelineReset, error) {
	items, err := s.timeline.ListTimelineForOwner(ctx, strings.TrimSpace(conversationID), strings.TrimSpace(ownerID))
	if err != nil {
		return TimelineReset{}, err
	}
	out := make([]TimelineItem, 0, len(items))
	for _, item := range items {
		out = append(out, TimelineItemFromRaw(item))
	}
	return TimelineReset{
		Type:           "timeline.reset",
		ConversationID: strings.TrimSpace(conversationID),
		Items:          out,
	}, nil
}

func ParseClientMessage(payload map[string]any) (ClientMessage, error) {
	msg := ClientMessage{
		Type:           stringValue(payload["type"], ""),
		CommandID:      strings.TrimSpace(stringValue(payload["command_id"], "")),
		ConversationID: strings.TrimSpace(stringValue(payload["conversation_id"], "")),
	}
	switch msg.Type {
	case "workspace.ping":
		return msg, nil
	case "timeline.subscribe", "timeline.unsubscribe":
		if msg.ConversationID == "" {
			return ClientMessage{}, ErrInvalidClientMessage
		}
		return msg, nil
	case "automation.record.start":
		if msg.CommandID == "" || msg.ConversationID == "" {
			return ClientMessage{}, ErrInvalidClientMessage
		}
		return msg, nil
	case "automation.record.stop", "automation.record.cancel", "automation.record.get":
		if msg.CommandID == "" {
			return ClientMessage{}, ErrInvalidClientMessage
		}
		return msg, nil
	case "workspace.command":
		commandRaw, _ := payload["command"].(map[string]any)
		command := &Command{
			ID:                  stringValue(commandRaw["command_id"], ""),
			Kind:                stringValue(commandRaw["kind"], ""),
			ConversationID:      strings.TrimSpace(stringValue(commandRaw["conversation_id"], msg.ConversationID)),
			RequestID:           strings.TrimSpace(stringValue(commandRaw["request_id"], "")),
			Text:                stringValue(commandRaw["text"], ""),
			Mode:                stringValue(commandRaw["mode"], ""),
			ToolName:            stringValue(commandRaw["tool_name"], ""),
			ToolCallID:          stringValue(commandRaw["tool_call_id"], ""),
			Output:              stringValue(commandRaw["output"], ""),
			BuiltinToolKind:     stringValue(commandRaw["builtin_tool_kind"], ""),
			BuiltinToolQuery:    stringValue(commandRaw["builtin_tool_query"], ""),
			BuiltinToolAssetID:  stringValue(commandRaw["builtin_tool_asset_id"], ""),
			ReasoningStreamMode: stringValue(commandRaw["reasoning_stream_mode"], ""),
			Error:               stringValue(commandRaw["error"], ""),
		}
		if command.ID == "" || command.Kind == "" || command.ConversationID == "" || command.RequestID == "" {
			return ClientMessage{}, ErrInvalidClientMessage
		}
		msg.Command = command
		msg.ConversationID = command.ConversationID
		return msg, nil
	default:
		return ClientMessage{}, ErrInvalidClientMessage
	}
}

type RealtimePublisher struct {
	hub *Hub
}

type AutomationRealtimePublisher struct{ hub *Hub }

func NewAutomationRealtimePublisher(hub *Hub) *AutomationRealtimePublisher {
	return &AutomationRealtimePublisher{hub: hub}
}

func (p *AutomationRealtimePublisher) HandleAutomationState(_ context.Context, event automationsvc.StateEvent) {
	if p == nil || p.hub == nil || strings.TrimSpace(event.OwnerID) == "" {
		return
	}
	if event.Recording != nil {
		p.hub.broadcast(event.OwnerID, map[string]any{"type": "automation.record.state", "state": event.Recording}, nil)
	}
	if event.Execution != nil {
		p.hub.broadcast(event.OwnerID, map[string]any{"type": "automation.execution.state", "execution": event.Execution}, nil)
	}
}

func NewRealtimePublisher(hub *Hub) *RealtimePublisher {
	return &RealtimePublisher{hub: hub}
}

func (p *RealtimePublisher) HandleChatEvent(ctx context.Context, event chatevents.Event) {
	if p == nil || p.hub == nil {
		return
	}
	switch event.Type {
	case chatevents.TypeConversationUpserted:
		p.publishConversationUpsert(event.OwnerID, event.Conversation)
	case chatevents.TypeConversationDeleted:
		p.publishConversationDelete(event.OwnerID, event.ConversationID)
	case chatevents.TypeMessageAppended, chatevents.TypeConversationEventAppended:
		item, ok := timelineItemFromChatEvent(event)
		if !ok {
			return
		}
		p.publishTimelineItemAppend(event.OwnerID, event.Conversation, item)
	}
	_ = ctx
}

func (p *RealtimePublisher) publishConversationUpsert(ownerID string, conversation common.Conversation) {
	if p == nil || p.hub == nil {
		return
	}
	if ownerID == "" {
		return
	}
	p.hub.publishConversationUpsert(ownerID, conversation)
}

func (p *RealtimePublisher) publishConversationDelete(ownerID string, conversationID string) {
	if p == nil || p.hub == nil {
		return
	}
	p.hub.publishConversationDelete(strings.TrimSpace(ownerID), strings.TrimSpace(conversationID))
}

func (p *RealtimePublisher) publishTimelineItemAppend(ownerID string, conversation common.Conversation, item timelinesvc.Item) {
	if p == nil || p.hub == nil {
		return
	}
	p.hub.publishTimelineItemAppend(strings.TrimSpace(ownerID), conversation, item)
}

func timelineItemFromChatEvent(event chatevents.Event) (timelinesvc.Item, bool) {
	switch {
	case event.Message != nil:
		return timelinesvc.ItemFromMessage(*event.Message), true
	case event.ConversationEvent != nil:
		return timelinesvc.ItemFromConversationEvent(*event.ConversationEvent), true
	default:
		return timelinesvc.Item{}, false
	}
}

type Hub struct {
	workspace *Service

	mu          sync.RWMutex
	connections map[string]map[*Connection]struct{}
}

func NewHub(workspace *Service) *Hub {
	return &Hub{
		workspace:   workspace,
		connections: map[string]map[*Connection]struct{}{},
	}
}

func (h *Hub) Register(ownerID string, conn *Connection) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.connections[ownerID]; !ok {
		h.connections[ownerID] = map[*Connection]struct{}{}
	}
	h.connections[ownerID][conn] = struct{}{}
	return len(h.connections[ownerID])
}

func (h *Hub) Unregister(ownerID string, conn *Connection) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if items, ok := h.connections[ownerID]; ok {
		delete(items, conn)
		if len(items) == 0 {
			delete(h.connections, ownerID)
			return 0
		}
		return len(items)
	}
	return 0
}

func (h *Hub) Snapshot(ctx context.Context, ownerID string) (Snapshot, error) {
	return h.workspace.Snapshot(ctx, ownerID)
}

func (h *Hub) HandleClientMessage(ctx context.Context, ownerID string, conn *Connection, msg ClientMessage) error {
	switch msg.Type {
	case "workspace.ping":
		conn.Send(map[string]any{"type": "workspace.ping"})
		return nil
	case "timeline.subscribe":
		conn.Lock()
		defer conn.Unlock()
		conn.Subscribe(msg.ConversationID)
		reset, err := h.workspace.TimelineReset(ctx, ownerID, msg.ConversationID)
		if err != nil {
			conn.Unsubscribe(msg.ConversationID)
			return err
		}
		conn.SendLocked(reset)
		return nil
	case "timeline.unsubscribe":
		conn.Unsubscribe(msg.ConversationID)
		return nil
	case "workspace.command":
		if msg.Command == nil {
			return ErrInvalidClientMessage
		}
		ack, err := h.workspace.ExecuteCommand(ctx, ownerID, *msg.Command)
		if err != nil {
			code, message := commandErrorPayload(err)
			conn.Send(CommandError{
				Type:           "workspace.command_error",
				CommandID:      msg.Command.ID,
				ConversationID: msg.Command.ConversationID,
				RequestID:      msg.Command.RequestID,
				Code:           code,
				Message:        message,
			})
			return nil
		}
		conn.Send(ack)
		return nil
	case "automation.record.start", "automation.record.stop", "automation.record.cancel", "automation.record.get":
		if h.workspace == nil || h.workspace.automation == nil {
			conn.Send(map[string]any{"type": "automation.record.error", "command_id": msg.CommandID, "code": "automation_unavailable", "message": "automation recorder unavailable"})
			return nil
		}
		var state automationsvc.RecordingState
		var err error
		switch msg.Type {
		case "automation.record.start":
			state, err = h.workspace.automation.StartRecording(ctx, ownerID, msg.ConversationID)
		case "automation.record.stop":
			state, err = h.workspace.automation.StopRecording(ctx, ownerID)
		case "automation.record.cancel":
			state, err = h.workspace.automation.CancelRecording(ctx, ownerID)
		default:
		}
		if err != nil {
			conn.Send(map[string]any{"type": "automation.record.error", "command_id": msg.CommandID, "code": "automation_record_failed", "message": err.Error()})
			return nil
		}
		snapshot := h.workspace.automation.StateSnapshot(ownerID)
		if msg.Type == "automation.record.get" {
			state = snapshot.Recording
		}
		conn.Send(map[string]any{
			"type": "automation.record.ack", "command_id": msg.CommandID, "state": state,
			"revision": snapshot.Revision, "executions": snapshot.Executions,
		})
		return nil
	default:
		return ErrInvalidClientMessage
	}
}

func (s *Service) ExecuteCommand(ctx context.Context, ownerID string, command Command) (CommandAck, error) {
	if s == nil || s.turn == nil {
		return CommandAck{}, fmt.Errorf("workspace command executor unavailable")
	}
	kind := turnsvc.TurnControlKind(strings.TrimSpace(command.Kind))
	result, err := s.turn.Execute(ctx, controlsvc.Command{
		OwnerID:        strings.TrimSpace(ownerID),
		ConversationID: strings.TrimSpace(command.ConversationID),
		RequestID:      strings.TrimSpace(command.RequestID),
		Source:         controlsvc.SourceWorkspace,
		Action: turnsvc.OutputAction{
			Kind:                kind,
			OutputText:          command.Text,
			Mode:                strings.TrimSpace(command.Mode),
			ToolName:            strings.TrimSpace(command.ToolName),
			ToolCallID:          strings.TrimSpace(command.ToolCallID),
			ToolOutput:          command.Output,
			BuiltinToolKind:     strings.TrimSpace(command.BuiltinToolKind),
			BuiltinToolQuery:    strings.TrimSpace(command.BuiltinToolQuery),
			BuiltinToolAssetID:  strings.TrimSpace(command.BuiltinToolAssetID),
			ReasoningStreamMode: strings.TrimSpace(command.ReasoningStreamMode),
			AbortReason:         strings.TrimSpace(command.Error),
		},
	})
	if err != nil {
		return CommandAck{}, err
	}
	return CommandAck{
		Type:           "workspace.command_ack",
		CommandID:      command.ID,
		ConversationID: strings.TrimSpace(command.ConversationID),
		RequestID:      strings.TrimSpace(command.RequestID),
		AutoCompleted:  boolValue(result.Body["auto_completed"]),
	}, nil
}

func boolValue(value any) bool {
	valueBool, _ := value.(bool)
	return valueBool
}

func commandErrorPayload(err error) (string, string) {
	var controlErr *controlsvc.Error
	if errors.As(err, &controlErr) {
		return controlErr.Code, controlErr.Message
	}
	if err == nil {
		return "workspace_command_failed", "workspace command failed"
	}
	return "workspace_command_failed", err.Error()
}

func (h *Hub) ConnectionCount(ownerID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections[ownerID])
}

func (h *Hub) publishConversationUpsert(ownerID string, conversation common.Conversation) {
	h.broadcast(ownerID, ConversationUpsert{
		Type:         "conversation.upsert",
		Conversation: SummaryFromConversation(conversation),
	}, nil)
}

func (h *Hub) publishConversationDelete(ownerID string, conversationID string) {
	h.broadcast(ownerID, ConversationDelete{
		Type:           "conversation.remove",
		ConversationID: conversationID,
	}, nil)
}

func (h *Hub) publishTimelineItemAppend(ownerID string, conversation common.Conversation, item timelinesvc.Item) {
	h.broadcast(ownerID, TimelineItemAppend{
		Type:           "timeline.append",
		ConversationID: conversation.ID,
		Item:           TimelineItemFromRaw(item),
	}, func(conn *Connection) bool {
		return conn.IsSubscribed(conversation.ID)
	})
}

func (h *Hub) PublishConnectionCount(ownerID string) {
	h.broadcast(ownerID, ConnectionCount{
		Type:                   "workspace.connections",
		CurrentConnectionCount: h.ConnectionCount(ownerID),
	}, nil)
}

func (h *Hub) broadcast(ownerID string, payload any, allow func(*Connection) bool) {
	h.mu.RLock()
	connections := h.connections[ownerID]
	if len(connections) == 0 {
		h.mu.RUnlock()
		return
	}
	items := make([]*Connection, 0, len(connections))
	for conn := range connections {
		items = append(items, conn)
	}
	h.mu.RUnlock()
	for _, conn := range items {
		if allow != nil && !allow(conn) {
			continue
		}
		conn.Send(payload)
	}
}

type Connection struct {
	mu            sync.Mutex
	send          func(any)
	subscriptions map[string]struct{}
}

func NewConnection(send func(any)) *Connection {
	return &Connection{
		send:          send,
		subscriptions: map[string]struct{}{},
	}
}

func (c *Connection) Lock() {
	if c == nil {
		return
	}
	c.mu.Lock()
}

func (c *Connection) Unlock() {
	if c == nil {
		return
	}
	c.mu.Unlock()
}

func (c *Connection) Send(payload any) {
	if c == nil || c.send == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.send(payload)
}

func (c *Connection) SendLocked(payload any) {
	if c == nil || c.send == nil {
		return
	}
	c.send(payload)
}

func (c *Connection) Subscribe(conversationID string) {
	if c == nil {
		return
	}
	c.subscriptions[strings.TrimSpace(conversationID)] = struct{}{}
}

func (c *Connection) Unsubscribe(conversationID string) {
	if c == nil {
		return
	}
	delete(c.subscriptions, strings.TrimSpace(conversationID))
}

func (c *Connection) IsSubscribed(conversationID string) bool {
	if c == nil {
		return false
	}
	_, ok := c.subscriptions[strings.TrimSpace(conversationID)]
	return ok
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}
