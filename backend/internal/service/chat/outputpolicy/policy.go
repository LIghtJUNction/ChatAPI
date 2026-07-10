package outputpolicy

import (
	"strconv"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/protocol"
)

type AppliedPolicy struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Value        string `json:"value,omitempty"`
	SupportLevel string `json:"support_level"`
}

type Input struct {
	Request      protocol.TurnRequest
	ExistingText string
	Text         string
	Mode         string
}

type Result struct {
	Text               string
	StopHit            bool
	StopSequence       string
	MaxOutputHit       bool
	MaxOutputChars     int
	StoreFalse         bool
	StoreApplied       bool
	AppliedPolicyChips []AppliedPolicy
}

func Apply(input Input) Result {
	result := Result{Text: input.Text}
	mode := normalizedMode(input.Mode)
	if mode == "tool_call" || mode == "tool_result" {
		result.addStorePolicy(input.Request.Options)
		return result
	}

	result.Text, result.StopHit, result.StopSequence = applyStop(result.Text, input.Request.Options.Stop)
	if result.StopHit {
		result.AppliedPolicyChips = append(result.AppliedPolicyChips, AppliedPolicy{
			Key:          "stop_hit",
			Label:        "stop hit",
			Value:        result.StopSequence,
			SupportLevel: "applied",
		})
	}

	if maxChars := maxOutputChars(input.Request.Options); maxChars > 0 {
		remaining := maxChars - len([]rune(input.ExistingText))
		if remaining < 0 {
			remaining = 0
		}
		if len([]rune(result.Text)) > remaining {
			result.Text = truncateRunes(result.Text, remaining)
			result.MaxOutputHit = true
			result.MaxOutputChars = maxChars
			result.AppliedPolicyChips = append(result.AppliedPolicyChips, AppliedPolicy{
				Key:          "max_out_enforced",
				Label:        "max out",
				Value:        strconv.Itoa(maxChars),
				SupportLevel: "applied",
			})
		}
	}

	result.addStorePolicy(input.Request.Options)
	return result
}

func (r *Result) addStorePolicy(options protocol.TurnOptions) {
	if options.Store == nil || *options.Store {
		return
	}
	r.StoreFalse = true
	r.StoreApplied = false
	r.AppliedPolicyChips = append(r.AppliedPolicyChips, AppliedPolicy{
		Key:          "store_false_local",
		Label:        "store=false",
		Value:        "local history retained",
		SupportLevel: "partially_applied",
	})
}

func (r Result) Metadata() map[string]any {
	if !r.StopHit && !r.MaxOutputHit && !r.StoreFalse && len(r.AppliedPolicyChips) == 0 {
		return nil
	}
	out := map[string]any{}
	if r.StopHit {
		out["stop_hit"] = true
		out["stop_sequence"] = r.StopSequence
	}
	if r.MaxOutputHit {
		out["max_output_hit"] = true
		out["max_output_chars"] = r.MaxOutputChars
	}
	if r.StoreFalse {
		out["store_false"] = true
		out["store_applied"] = r.StoreApplied
	}
	if len(r.AppliedPolicyChips) > 0 {
		chips := make([]map[string]any, 0, len(r.AppliedPolicyChips))
		for _, chip := range r.AppliedPolicyChips {
			chips = append(chips, map[string]any{
				"key":           chip.Key,
				"label":         chip.Label,
				"value":         chip.Value,
				"support_level": chip.SupportLevel,
			})
		}
		out["applied_chips"] = chips
	}
	return out
}

func applyStop(text string, stops []string) (string, bool, string) {
	bestIndex := -1
	bestStop := ""
	for _, stop := range stops {
		stop = strings.TrimSpace(stop)
		if stop == "" {
			continue
		}
		index := strings.Index(text, stop)
		if index < 0 {
			continue
		}
		if bestIndex < 0 || index < bestIndex {
			bestIndex = index
			bestStop = stop
		}
	}
	if bestIndex < 0 {
		return text, false, ""
	}
	return text[:bestIndex], true, bestStop
}

func maxOutputChars(options protocol.TurnOptions) int {
	for _, value := range []*int{options.MaxOutputTokens, options.MaxCompletionTokens, options.MaxTokens} {
		if value != nil && *value > 0 {
			return *value
		}
	}
	return 0
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func normalizedMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "tool_call":
		return "tool_call"
	case "tool_result":
		return "tool_result"
	default:
		return "assistant_message"
	}
}
