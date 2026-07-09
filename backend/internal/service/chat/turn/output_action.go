package turn

import (
	"errors"
	"strings"

	protocolruntime "github.com/zyf2007/ChatAPI/internal/service/chat/protocolruntime"
)

type OutputAction struct {
	Kind                TurnControlKind
	OutputText          string
	Mode                string
	ToolName            string
	ToolCallID          string
	ToolOutput          string
	BuiltinToolKind     string
	BuiltinToolQuery    string
	BuiltinToolResult   string
	ReasoningStreamMode string
	AbortReason         string
}

func (a OutputAction) Normalized() OutputAction {
	a.Kind = TurnControlKind(strings.TrimSpace(string(a.Kind)))
	a.OutputText = strings.TrimSpace(a.OutputText)
	a.Mode = strings.TrimSpace(a.Mode)
	if a.Mode == "" {
		a.Mode = "assistant_message"
	}
	a.ToolName = strings.TrimSpace(a.ToolName)
	a.ToolCallID = strings.TrimSpace(a.ToolCallID)
	a.ToolOutput = strings.TrimSpace(a.ToolOutput)
	if a.ToolOutput == "" {
		a.ToolOutput = a.OutputText
	}
	a.BuiltinToolKind = strings.TrimSpace(a.BuiltinToolKind)
	a.BuiltinToolQuery = strings.TrimSpace(a.BuiltinToolQuery)
	a.BuiltinToolResult = strings.TrimSpace(a.BuiltinToolResult)
	a.ReasoningStreamMode = strings.TrimSpace(a.ReasoningStreamMode)
	a.AbortReason = strings.TrimSpace(a.AbortReason)
	return a
}

func (a OutputAction) Validate() error {
	a = a.Normalized()
	switch a.Kind {
	case TurnControlRespond, TurnControlStreamComplete, TurnControlStreamDelta:
		return nil
	case TurnControlBuiltinTool:
		spec, ok := protocolruntime.LookupBuiltinToolSpec(a.BuiltinToolKind)
		if !ok {
			return errors.New("unsupported builtin tool kind: " + a.BuiltinToolKind)
		}
		if spec.RequiresQuery && a.BuiltinToolQuery == "" {
			return errors.New("builtin_tool_query is required")
		}
		if spec.RequiresResult && a.BuiltinToolResult == "" {
			return errors.New("builtin_tool_result is required")
		}
		return nil
	case TurnControlAbort:
		if a.AbortReason == "" {
			return errors.New("error is required")
		}
		return nil
	default:
		return errors.New("unsupported turn control kind: " + string(a.Kind))
	}
}

func (a OutputAction) RuntimeKind() protocolruntime.ActionKind {
	switch a.Normalized().Kind {
	case TurnControlStreamDelta:
		return protocolruntime.ActionDelta
	case TurnControlBuiltinTool:
		return protocolruntime.ActionBuiltin
	case TurnControlAbort:
		return protocolruntime.ActionAbort
	default:
		return protocolruntime.ActionComplete
	}
}

func (a OutputAction) RuntimeAction() protocolruntime.Action {
	a = a.Normalized()
	return protocolruntime.Action{
		Kind:                a.RuntimeKind(),
		DeltaText:           a.OutputText,
		OutputText:          a.OutputText,
		Mode:                a.Mode,
		ReasoningStreamMode: a.ReasoningStreamMode,
		ToolName:            a.ToolName,
		ToolCallID:          a.ToolCallID,
		ToolOutput:          a.ToolOutput,
		BuiltinToolKind:     a.BuiltinToolKind,
		BuiltinToolQuery:    a.BuiltinToolQuery,
		BuiltinToolResult:   a.BuiltinToolResult,
	}
}
