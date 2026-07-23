package workspace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	automationsvc "github.com/zyf2007/ChatAPI/internal/service/automation"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	workspacesettings "github.com/zyf2007/ChatAPI/internal/service/chat/workspace/settings"
)

type ConversationQuery interface {
	ListConversationsForOwnerPage(context.Context, string, time.Time, string, int) ([]common.Conversation, error)
	CountConversationsForOwner(context.Context, string) (int, error)
}

type TimelineQuery interface {
	ListTimelineForOwner(context.Context, string, string) ([]timelinesvc.Item, error)
}

type Snapshot struct {
	Type              string                `json:"type"`
	Conversations     []ConversationSummary `json:"conversations"`
	ConversationCount int                   `json:"conversation_count"`
	HasMore           bool                  `json:"has_more"`
	NextCursor        string                `json:"next_cursor,omitempty"`
}

type ConversationPage struct {
	Type          string                `json:"type"`
	CommandID     string                `json:"command_id"`
	Conversations []ConversationSummary `json:"conversations"`
	HasMore       bool                  `json:"has_more"`
	NextCursor    string                `json:"next_cursor,omitempty"`
}

type ConversationUpsert struct {
	Type              string              `json:"type"`
	Conversation      ConversationSummary `json:"conversation"`
	ConversationCount *int                `json:"conversation_count,omitempty"`
}

type ConversationDelete struct {
	Type              string `json:"type"`
	ConversationID    string `json:"conversation_id"`
	ConversationCount *int   `json:"conversation_count,omitempty"`
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
	Cursor         string   `json:"cursor,omitempty"`
}

const conversationPageSize = 30

type conversationCursor struct {
	OwnerID   string    `json:"owner_id"`
	UpdatedAt time.Time `json:"updated_at"`
	ID        string    `json:"id"`
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
	ownerID = strings.TrimSpace(ownerID)
	page, err := s.conversationPage(ctx, ownerID, conversationCursor{})
	if err != nil {
		return Snapshot{}, err
	}
	count, err := s.conversations.CountConversationsForOwner(ctx, ownerID)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Type: "workspace.snapshot", Conversations: page.Conversations, ConversationCount: count, HasMore: page.HasMore, NextCursor: page.NextCursor}, nil
}

func (s *Service) ConversationPage(ctx context.Context, ownerID, commandID, rawCursor string) (ConversationPage, error) {
	cursor, err := decodeConversationCursor(rawCursor, ownerID)
	if err != nil {
		return ConversationPage{}, err
	}
	page, err := s.conversationPage(ctx, ownerID, cursor)
	page.CommandID = commandID
	return page, err
}

func (s *Service) conversationPage(ctx context.Context, ownerID string, cursor conversationCursor) (ConversationPage, error) {
	items, err := s.conversations.ListConversationsForOwnerPage(ctx, ownerID, cursor.UpdatedAt, cursor.ID, conversationPageSize+1)
	if err != nil {
		return ConversationPage{}, err
	}
	hasMore := len(items) > conversationPageSize
	if hasMore {
		items = items[:conversationPageSize]
	}
	summaries := make([]ConversationSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, SummaryFromConversation(item))
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor, err = encodeConversationCursor(conversationCursor{OwnerID: ownerID, UpdatedAt: items[len(items)-1].UpdatedAt, ID: items[len(items)-1].ID})
		if err != nil {
			return ConversationPage{}, err
		}
	}
	return ConversationPage{Type: "conversation.page", Conversations: summaries, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func encodeConversationCursor(cursor conversationCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeConversationCursor(raw, ownerID string) (conversationCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return conversationCursor{}, ErrInvalidClientMessage
	}
	var cursor conversationCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.OwnerID != strings.TrimSpace(ownerID) || cursor.ID == "" || cursor.UpdatedAt.IsZero() {
		return conversationCursor{}, ErrInvalidClientMessage
	}
	return cursor, nil
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
		Cursor:         strings.TrimSpace(stringValue(payload["cursor"], "")),
	}
	switch msg.Type {
	case "workspace.ping":
		return msg, nil
	case "conversation.page.get":
		if msg.CommandID == "" || msg.Cursor == "" {
			return ClientMessage{}, ErrInvalidClientMessage
		}
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
			Text:                textValue(commandRaw["text"], ""),
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
		p.publishConversationUpsert(ctx, event.OwnerID, event.Conversation)
	case chatevents.TypeConversationDeleted:
		p.publishConversationDelete(ctx, event.OwnerID, event.ConversationID)
	case chatevents.TypeMessageAppended, chatevents.TypeConversationEventAppended:
		item, ok := timelineItemFromChatEvent(event)
		if !ok {
			return
		}
		p.publishTimelineItemAppend(event.OwnerID, event.Conversation, item)
	}
}

func (p *RealtimePublisher) publishConversationUpsert(ctx context.Context, ownerID string, conversation common.Conversation) {
	if p == nil || p.hub == nil {
		return
	}
	if ownerID == "" {
		return
	}
	p.hub.publishConversationUpsert(ownerID, conversation, p.conversationCount(ctx, ownerID))
}

func (p *RealtimePublisher) publishConversationDelete(ctx context.Context, ownerID string, conversationID string) {
	if p == nil || p.hub == nil {
		return
	}
	ownerID = strings.TrimSpace(ownerID)
	p.hub.publishConversationDelete(ownerID, strings.TrimSpace(conversationID), p.conversationCount(ctx, ownerID))
}

func (p *RealtimePublisher) conversationCount(ctx context.Context, ownerID string) *int {
	if p == nil || p.hub == nil || p.hub.workspace == nil || p.hub.workspace.conversations == nil {
		return nil
	}
	count, err := p.hub.workspace.conversations.CountConversationsForOwner(ctx, strings.TrimSpace(ownerID))
	if err != nil {
		return nil
	}
	return &count
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
	settings    *workspacesettings.Service
	presenceMu  sync.RWMutex
	presence    map[*presenceSubscription]struct{}
	presenceSeq uint64
}

func NewHub(workspace *Service) *Hub {
	return &Hub{
		workspace:   workspace,
		connections: map[string]map[*Connection]struct{}{},
		presence:    map[*presenceSubscription]struct{}{},
	}
}

func (h *Hub) SetSettings(settings *workspacesettings.Service) { h.settings = settings }

func (h *Hub) TryRegister(ctx context.Context, ownerID string, conn *Connection) (int, error) {
	var limits workspacesettings.Settings
	if h.settings != nil {
		settings, err := h.settings.Current(ctx)
		if err != nil {
			return 0, err
		}
		limits = settings
	}
	h.mu.Lock()
	if h.settings != nil {
		total := 0
		for _, items := range h.connections {
			total += len(items)
		}
		if limits.MaxConnections > 0 && total >= limits.MaxConnections {
			h.mu.Unlock()
			return 0, fmt.Errorf("realtime global connection limit reached")
		}
		if limits.MaxConnectionsPerUser > 0 && len(h.connections[ownerID]) >= limits.MaxConnectionsPerUser {
			h.mu.Unlock()
			return 0, fmt.Errorf("realtime user connection limit reached")
		}
	}
	if _, ok := h.connections[ownerID]; !ok {
		h.connections[ownerID] = map[*Connection]struct{}{}
	}
	h.connections[ownerID][conn] = struct{}{}
	count := len(h.connections[ownerID])
	total := h.totalConnectionsLocked()
	sequence := h.nextPresenceSequenceLocked()
	h.mu.Unlock()
	h.publishPresence(ownerID, count, total, sequence)
	return count, nil
}

func (h *Hub) Register(ownerID string, conn *Connection) int {
	h.mu.Lock()
	if _, ok := h.connections[ownerID]; !ok {
		h.connections[ownerID] = map[*Connection]struct{}{}
	}
	h.connections[ownerID][conn] = struct{}{}
	count := len(h.connections[ownerID])
	total := h.totalConnectionsLocked()
	sequence := h.nextPresenceSequenceLocked()
	h.mu.Unlock()
	h.publishPresence(ownerID, count, total, sequence)
	return count
}

func (h *Hub) Unregister(ownerID string, conn *Connection) int {
	h.mu.Lock()
	count := 0
	if items, ok := h.connections[ownerID]; ok {
		delete(items, conn)
		if len(items) == 0 {
			delete(h.connections, ownerID)
		} else {
			count = len(items)
		}
	}
	total := h.totalConnectionsLocked()
	sequence := h.nextPresenceSequenceLocked()
	h.mu.Unlock()
	h.publishPresence(ownerID, count, total, sequence)
	return count
}

func (h *Hub) Snapshot(ctx context.Context, ownerID string) (Snapshot, error) {
	return h.workspace.Snapshot(ctx, ownerID)
}

func (h *Hub) HandleClientMessage(ctx context.Context, ownerID string, conn *Connection, msg ClientMessage) error {
	switch msg.Type {
	case "workspace.ping":
		conn.Send(map[string]any{"type": "workspace.ping"})
		return nil
	case "conversation.page.get":
		page, err := h.workspace.ConversationPage(ctx, ownerID, msg.CommandID, msg.Cursor)
		if err != nil {
			code, message := "page_failed", "conversation page could not be loaded"
			if errors.Is(err, ErrInvalidClientMessage) {
				code, message = "invalid_cursor", "conversation page cursor is invalid"
			}
			conn.Send(map[string]any{"type": "conversation.page.error", "command_id": msg.CommandID, "code": code, "message": message})
			return nil
		}
		conn.Send(page)
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
			OutputText:          workspaceOutputText(command.Text),
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

func workspaceOutputText(value string) string {
	value = strings.ReplaceAll(value, `\r\n`, "\n")
	return strings.ReplaceAll(value, `\n`, "\n")
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

func (h *Hub) publishConversationUpsert(ownerID string, conversation common.Conversation, count *int) {
	h.broadcast(ownerID, ConversationUpsert{
		Type:              "conversation.upsert",
		Conversation:      SummaryFromConversation(conversation),
		ConversationCount: count,
	}, nil)
}

func (h *Hub) publishConversationDelete(ownerID string, conversationID string, count *int) {
	h.broadcast(ownerID, ConversationDelete{
		Type:              "conversation.remove",
		ConversationID:    conversationID,
		ConversationCount: count,
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
	initializing  bool
	pending       []any
}

func (c *Connection) BeginInitialization() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.initializing = true
	c.pending = nil
	c.mu.Unlock()
}

func (c *Connection) Activate(initial any) {
	if c == nil || c.send == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.send(initial)
	for _, payload := range c.pending {
		c.send(payload)
	}
	c.pending = nil
	c.initializing = false
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
	if c.initializing {
		c.pending = append(c.pending, payload)
		return
	}
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

func textValue(value any, fallback string) string {
	if raw, ok := value.(string); ok {
		return raw
	}
	return fallback
}
