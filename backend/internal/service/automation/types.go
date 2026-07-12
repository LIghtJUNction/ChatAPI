package automation

import (
	"strings"
	"time"

	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

const SchemaVersion = 2

type MatchSpec struct {
	Target  string `json:"target"`
	Pattern string `json:"pattern"`
}

type PlaybackSpec struct {
	Mode            string `json:"mode"`
	InitialDelayMS  int64  `json:"initial_delay_ms"`
	FixedIntervalMS int64  `json:"fixed_interval_ms"`
	Loop            bool   `json:"loop"`
	LoopIntervalMS  int64  `json:"loop_interval_ms"`
}

type Action struct {
	Kind                string `json:"kind"`
	Text                string `json:"text,omitempty"`
	Mode                string `json:"mode,omitempty"`
	ToolName            string `json:"tool_name,omitempty"`
	ToolCallID          string `json:"tool_call_id,omitempty"`
	Output              string `json:"output,omitempty"`
	BuiltinToolKind     string `json:"builtin_tool_kind,omitempty"`
	BuiltinToolQuery    string `json:"builtin_tool_query,omitempty"`
	BuiltinToolAssetID  string `json:"builtin_tool_asset_id,omitempty"`
	ReasoningStreamMode string `json:"reasoning_stream_mode,omitempty"`
	Error               string `json:"error,omitempty"`
}

func ActionFromTurn(value turnsvc.OutputAction) Action {
	value = value.Normalized()
	return Action{
		Kind:                string(value.Kind),
		Text:                value.OutputText,
		Mode:                value.Mode,
		ToolName:            value.ToolName,
		ToolCallID:          value.ToolCallID,
		Output:              value.ToolOutput,
		BuiltinToolKind:     value.BuiltinToolKind,
		BuiltinToolQuery:    value.BuiltinToolQuery,
		BuiltinToolAssetID:  value.BuiltinToolAssetID,
		ReasoningStreamMode: value.ReasoningStreamMode,
		Error:               value.AbortReason,
	}
}

func (a Action) TurnAction() turnsvc.OutputAction {
	return turnsvc.OutputAction{
		Kind:                turnsvc.TurnControlKind(strings.TrimSpace(a.Kind)),
		OutputText:          a.Text,
		Mode:                a.Mode,
		ToolName:            a.ToolName,
		ToolCallID:          a.ToolCallID,
		ToolOutput:          a.Output,
		BuiltinToolKind:     a.BuiltinToolKind,
		BuiltinToolQuery:    a.BuiltinToolQuery,
		BuiltinToolAssetID:  a.BuiltinToolAssetID,
		ReasoningStreamMode: a.ReasoningStreamMode,
		AbortReason:         a.Error,
	}.Normalized()
}

func (a Action) Terminal() bool {
	switch turnsvc.TurnControlKind(strings.TrimSpace(a.Kind)) {
	case turnsvc.TurnControlStreamComplete, turnsvc.TurnControlRespond, turnsvc.TurnControlAbort:
		return true
	default:
		return false
	}
}

func (a Action) Recordable() bool {
	return !(strings.TrimSpace(a.Kind) == string(turnsvc.TurnControlBuiltinTool) && strings.TrimSpace(a.BuiltinToolKind) == "image_generation")
}

type Step struct {
	ID            string `json:"id"`
	DelayBeforeMS int64  `json:"delay_before_ms"`
	Action        Action `json:"action"`
}

type Rule struct {
	SchemaVersion int          `json:"schema_version"`
	ID            string       `json:"id"`
	OwnerID       string       `json:"owner_id,omitempty"`
	Name          string       `json:"name"`
	Enabled       bool         `json:"enabled"`
	Priority      int          `json:"priority"`
	Match         MatchSpec    `json:"match"`
	Playback      PlaybackSpec `json:"playback"`
	Steps         []Step       `json:"steps"`
	CreatedAt     time.Time    `json:"created_at,omitempty"`
	UpdatedAt     time.Time    `json:"updated_at,omitempty"`
}

type RecordingState struct {
	Revision       uint64    `json:"revision"`
	Active         bool      `json:"active"`
	OwnerID        string    `json:"-"`
	ConversationID string    `json:"conversation_id,omitempty"`
	RequestID      string    `json:"request_id,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	Steps          []Step    `json:"steps"`
	DraftRule      *Rule     `json:"draft_rule,omitempty"`
	Warning        string    `json:"warning,omitempty"`
}

type ExecutionState struct {
	Revision       uint64 `json:"revision"`
	OwnerID        string `json:"-"`
	RuleID         string `json:"rule_id"`
	ConversationID string `json:"conversation_id"`
	RequestID      string `json:"request_id"`
	Status         string `json:"status"`
	StepIndex      int    `json:"step_index"`
	StepCount      int    `json:"step_count"`
	Cycle          int    `json:"cycle"`
	Reason         string `json:"reason,omitempty"`
}

type StateSnapshot struct {
	Revision   uint64
	Recording  RecordingState
	Executions []ExecutionState
}
