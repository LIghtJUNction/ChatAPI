package debugview

import (
	"fmt"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/protocol"
)

type SupportLevel string

const (
	SupportApplied          SupportLevel = "applied"
	SupportNormalized       SupportLevel = "normalized"
	SupportStoredOnly       SupportLevel = "stored_only"
	SupportProviderSpecific SupportLevel = "provider_specific"
	SupportUnsupported      SupportLevel = "unsupported"
	SupportPartiallyApplied SupportLevel = "partially_applied"
)

type ChipCategory string

const (
	CategoryRequest          ChipCategory = "request"
	CategoryProviderSpecific ChipCategory = "provider_specific"
	CategoryUnsupported      ChipCategory = "unsupported"
)

type OptionChip struct {
	Key          string       `json:"key"`
	Label        string       `json:"label"`
	Value        string       `json:"value,omitempty"`
	Protocol     string       `json:"protocol,omitempty"`
	Category     ChipCategory `json:"category"`
	SupportLevel SupportLevel `json:"support_level"`
	Detail       any          `json:"detail,omitempty"`
}

type Projection struct {
	OptionChips  []OptionChip           `json:"option_chips,omitempty"`
	BuiltinTools []protocol.BuiltinTool `json:"builtin_tools,omitempty"`
}

func ProjectRequest(request protocol.TurnRequest) Projection {
	options := request.Options
	chips := make([]OptionChip, 0, 16)
	chips = appendValueChip(chips, request.Protocol, "temperature", "temp", floatValue(options.Temperature), SupportNormalized)
	chips = appendValueChip(chips, request.Protocol, "top_p", "top_p", floatValue(options.TopP), SupportNormalized)
	chips = appendValueChip(chips, request.Protocol, "top_k", "top_k", intValue(options.TopK), SupportNormalized)
	chips = appendValueChip(chips, request.Protocol, "seed", "seed", int64Value(options.Seed), SupportNormalized)
	chips = appendValueChip(chips, request.Protocol, "service_tier", "tier", options.ServiceTier, SupportStoredOnly)
	chips = appendValueChip(chips, request.Protocol, "user", "user", options.User, SupportStoredOnly)
	chips = appendValueChip(chips, request.Protocol, "max_output_tokens", "max_out", intValue(options.MaxOutputTokens), SupportNormalized)
	chips = appendValueChip(chips, request.Protocol, "max_tokens", "max", intValue(options.MaxTokens), SupportNormalized)
	chips = appendValueChip(chips, request.Protocol, "max_completion_tokens", "max_completion", intValue(options.MaxCompletionTokens), SupportNormalized)
	chips = appendBoolChip(chips, request.Protocol, "store", "store", options.Store, SupportPartiallyApplied)
	chips = appendBoolChip(chips, request.Protocol, "parallel_tool_calls", "parallel_tools", options.ParallelToolCalls, SupportPartiallyApplied)
	if len(options.Stop) > 0 {
		chips = append(chips, OptionChip{
			Key:          "stop",
			Label:        "stop",
			Value:        strings.Join(options.Stop, ", "),
			Protocol:     request.Protocol.String(),
			Category:     CategoryRequest,
			SupportLevel: SupportPartiallyApplied,
			Detail:       append([]string(nil), options.Stop...),
		})
	}
	if request.ResponseFormat.Type != "" {
		chips = appendObjectChip(chips, request.Protocol, "response_format", "format", request.ResponseFormat, SupportNormalized)
	}
	switch request.Protocol {
	case protocol.ProtocolResponses:
		chips = appendValueChip(chips, request.Protocol, "instructions", "instructions", shortPresent(options.Instructions), SupportStoredOnly)
		chips = appendValueChip(chips, request.Protocol, "previous_response_id", "prev_resp", shortPresent(options.PreviousResponseID), SupportProviderSpecific)
		chips = appendValueChip(chips, request.Protocol, "truncation", "truncate", options.Truncation, SupportProviderSpecific)
		chips = appendObjectChip(chips, request.Protocol, "include", "include", options.Include, SupportProviderSpecific)
		chips = appendObjectChip(chips, request.Protocol, "reasoning", "reasoning_cfg", options.Reasoning, SupportProviderSpecific)
		chips = appendObjectChip(chips, request.Protocol, "text", "text_cfg", options.Text, SupportProviderSpecific)
	case protocol.ProtocolChatCompletions:
		chips = appendValueChip(chips, request.Protocol, "reasoning_effort", "reasoning", options.ReasoningEffort, SupportProviderSpecific)
		chips = appendValueChip(chips, request.Protocol, "n", "n", intValue(options.N), supportForN(options.N))
		chips = appendObjectChip(chips, request.Protocol, "modalities", "modalities", options.Modalities, SupportProviderSpecific)
		chips = appendObjectChip(chips, request.Protocol, "audio", "audio_cfg", options.Audio, SupportProviderSpecific)
		chips = appendObjectChip(chips, request.Protocol, "prediction", "prediction", options.Prediction, SupportProviderSpecific)
	case protocol.ProtocolAnthropicMessages:
		chips = appendObjectChip(chips, request.Protocol, "thinking", "thinking", options.Thinking, SupportProviderSpecific)
		chips = appendObjectChip(chips, request.Protocol, "mcp_servers", "mcp", options.MCPServers, SupportProviderSpecific)
		chips = appendObjectChip(chips, request.Protocol, "context_management", "context_mgmt", options.ContextManagement, SupportProviderSpecific)
	}
	chips = appendObjectChip(chips, request.Protocol, "metadata", "metadata", options.Metadata, SupportStoredOnly)
	chips = appendObjectChip(chips, request.Protocol, "stream_options", "stream_opts", options.StreamOptions, SupportStoredOnly)
	chips = appendObjectChip(chips, request.Protocol, "provider_extras", "extras", options.ProviderExtras, SupportStoredOnly)
	return Projection{
		OptionChips:  chips,
		BuiltinTools: append([]protocol.BuiltinTool(nil), request.BuiltinTools...),
	}
}

func appendValueChip(chips []OptionChip, proto protocol.Protocol, key string, label string, value string, support SupportLevel) []OptionChip {
	if strings.TrimSpace(value) == "" {
		return chips
	}
	category := CategoryRequest
	if support == SupportProviderSpecific {
		category = CategoryProviderSpecific
	}
	if support == SupportUnsupported {
		category = CategoryUnsupported
	}
	return append(chips, OptionChip{Key: key, Label: label, Value: value, Protocol: proto.String(), Category: category, SupportLevel: support})
}

func appendBoolChip(chips []OptionChip, proto protocol.Protocol, key string, label string, value *bool, support SupportLevel) []OptionChip {
	if value == nil {
		return chips
	}
	return appendValueChip(chips, proto, key, label, fmt.Sprintf("%t", *value), support)
}

func appendObjectChip(chips []OptionChip, proto protocol.Protocol, key string, label string, detail any, support SupportLevel) []OptionChip {
	if isEmptyDetail(detail) {
		return chips
	}
	category := CategoryRequest
	if support == SupportProviderSpecific {
		category = CategoryProviderSpecific
	}
	return append(chips, OptionChip{Key: key, Label: label, Value: "set", Protocol: proto.String(), Category: category, SupportLevel: support, Detail: detail})
}

func supportForN(value *int) SupportLevel {
	if value != nil && *value > 1 {
		return SupportUnsupported
	}
	return SupportProviderSpecific
}

func shortPresent(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "set"
}

func floatValue(value *float64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%g", *value)
}

func intValue(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func int64Value(value *int64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func isEmptyDetail(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []string:
		return len(typed) == 0
	case []map[string]any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}
