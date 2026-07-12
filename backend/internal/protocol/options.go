package protocol

import "encoding/json"

type TurnOptions struct {
	Instructions        string           `json:"instructions,omitempty"`
	PreviousResponseID  string           `json:"previous_response_id,omitempty"`
	Store               *bool            `json:"store,omitempty"`
	Metadata            map[string]any   `json:"metadata,omitempty"`
	Include             []string         `json:"include,omitempty"`
	MaxOutputTokens     *int             `json:"max_output_tokens,omitempty"`
	MaxTokens           *int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int             `json:"max_completion_tokens,omitempty"`
	Temperature         *float64         `json:"temperature,omitempty"`
	TopP                *float64         `json:"top_p,omitempty"`
	TopK                *int             `json:"top_k,omitempty"`
	Stop                []string         `json:"stop,omitempty"`
	N                   *int             `json:"n,omitempty"`
	PresencePenalty     *float64         `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64         `json:"frequency_penalty,omitempty"`
	Seed                *int64           `json:"seed,omitempty"`
	User                string           `json:"user,omitempty"`
	StreamOptions       map[string]any   `json:"stream_options,omitempty"`
	ParallelToolCalls   *bool            `json:"parallel_tool_calls,omitempty"`
	Reasoning           map[string]any   `json:"reasoning,omitempty"`
	ReasoningEffort     string           `json:"reasoning_effort,omitempty"`
	Thinking            map[string]any   `json:"thinking,omitempty"`
	ServiceTier         string           `json:"service_tier,omitempty"`
	Text                map[string]any   `json:"text,omitempty"`
	Truncation          string           `json:"truncation,omitempty"`
	Modalities          []string         `json:"modalities,omitempty"`
	Audio               map[string]any   `json:"audio,omitempty"`
	Prediction          map[string]any   `json:"prediction,omitempty"`
	MCPServers          []map[string]any `json:"mcp_servers,omitempty"`
	ContextManagement   map[string]any   `json:"context_management,omitempty"`
	ProviderExtras      map[string]any   `json:"provider_extras,omitempty"`
}

func extractTurnOptions(proto Protocol, body map[string]any) TurnOptions {
	options := TurnOptions{
		Metadata:          cloneAnyMap(firstMap(body["metadata"])),
		StreamOptions:     cloneAnyMap(firstMap(body["stream_options"])),
		ParallelToolCalls: boolPtrValue(body["parallel_tool_calls"]),
		ServiceTier:       stringValue(body["service_tier"], ""),
		User:              stringValue(body["user"], ""),
		ProviderExtras:    providerExtras(proto, body),
	}
	options.Temperature = floatPtrValue(body["temperature"])
	options.TopP = floatPtrValue(body["top_p"])

	switch proto {
	case ProtocolChatCompletions:
		options.MaxTokens = intPtrValue(body["max_tokens"])
		options.MaxCompletionTokens = intPtrValue(body["max_completion_tokens"])
		options.Stop = stringListValue(body["stop"])
		options.N = intPtrValue(body["n"])
		options.PresencePenalty = floatPtrValue(body["presence_penalty"])
		options.FrequencyPenalty = floatPtrValue(body["frequency_penalty"])
		options.Seed = int64PtrValue(body["seed"])
		options.ReasoningEffort = stringValue(body["reasoning_effort"], "")
		options.Modalities = stringListValue(body["modalities"])
		options.Audio = cloneAnyMap(firstMap(body["audio"]))
		options.Prediction = cloneAnyMap(firstMap(body["prediction"]))
	case ProtocolAnthropicMessages:
		options.MaxTokens = intPtrValue(body["max_tokens"])
		options.TopK = intPtrValue(body["top_k"])
		options.Stop = stringListValue(body["stop_sequences"])
		options.Thinking = cloneAnyMap(firstMap(body["thinking"]))
		options.MCPServers = mapListValue(body["mcp_servers"])
		options.ContextManagement = cloneAnyMap(firstMap(body["context_management"]))
	default:
		options.Instructions = stringValue(body["instructions"], "")
		options.PreviousResponseID = stringValue(body["previous_response_id"], "")
		options.Store = boolPtrValue(body["store"])
		options.Include = stringListValue(body["include"])
		options.MaxOutputTokens = intPtrValue(body["max_output_tokens"])
		options.Reasoning = cloneAnyMap(firstMap(body["reasoning"]))
		options.Text = cloneAnyMap(firstMap(body["text"]))
		options.Truncation = stringValue(body["truncation"], "")
	}
	return options
}

func cloneTurnOptions(input TurnOptions) TurnOptions {
	cloned := input
	cloned.Metadata = cloneAnyMap(input.Metadata)
	cloned.Include = append([]string(nil), input.Include...)
	cloned.Stop = append([]string(nil), input.Stop...)
	cloned.StreamOptions = cloneAnyMap(input.StreamOptions)
	cloned.Reasoning = cloneAnyMap(input.Reasoning)
	cloned.Thinking = cloneAnyMap(input.Thinking)
	cloned.Text = cloneAnyMap(input.Text)
	cloned.Modalities = append([]string(nil), input.Modalities...)
	cloned.Audio = cloneAnyMap(input.Audio)
	cloned.Prediction = cloneAnyMap(input.Prediction)
	cloned.MCPServers = cloneMapList(input.MCPServers)
	cloned.ContextManagement = cloneAnyMap(input.ContextManagement)
	cloned.ProviderExtras = cloneAnyMap(input.ProviderExtras)
	return cloned
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = cloneAnyValue(value)
	}
	return cloned
}

func cloneAnySlice(input []any) []any {
	if len(input) == 0 {
		return nil
	}
	cloned := make([]any, 0, len(input))
	for _, value := range input {
		cloned = append(cloned, cloneAnyValue(value))
	}
	return cloned
}

func cloneMapList(input []map[string]any) []map[string]any {
	if len(input) == 0 {
		return nil
	}
	cloned := make([]map[string]any, 0, len(input))
	for _, item := range input {
		cloned = append(cloned, cloneAnyMap(item))
	}
	return cloned
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		return cloneAnySlice(typed)
	case []map[string]any:
		return cloneMapList(typed)
	default:
		return typed
	}
}

func RequestOptionsDebug(request TurnRequest) map[string]any {
	return optionsDebugMap(request.Options)
}

func CloneTurnOptions(input TurnOptions) TurnOptions {
	return cloneTurnOptions(input)
}

func optionsDebugMap(options TurnOptions) map[string]any {
	raw, err := json.Marshal(options)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func providerExtras(proto Protocol, body map[string]any) map[string]any {
	known := knownRequestKeys(proto)
	extras := map[string]any{}
	for key, value := range body {
		if known[key] {
			continue
		}
		extras[key] = cloneAnyValue(value)
	}
	if len(extras) == 0 {
		return nil
	}
	return extras
}

func knownRequestKeys(proto Protocol) map[string]bool {
	keys := map[string]bool{
		"model":               true,
		"stream":              true,
		"conversation_id":     true,
		"tools":               true,
		"tool_choice":         true,
		"response_format":     true,
		"metadata":            true,
		"temperature":         true,
		"top_p":               true,
		"user":                true,
		"stream_options":      true,
		"parallel_tool_calls": true,
		"service_tier":        true,
	}
	switch proto {
	case ProtocolChatCompletions:
		for _, key := range []string{"messages", "max_tokens", "max_completion_tokens", "stop", "n", "presence_penalty", "frequency_penalty", "seed", "reasoning_effort", "modalities", "audio", "prediction"} {
			keys[key] = true
		}
	case ProtocolAnthropicMessages:
		for _, key := range []string{"messages", "system", "max_tokens", "top_k", "stop_sequences", "thinking", "mcp_servers", "context_management"} {
			keys[key] = true
		}
	default:
		for _, key := range []string{"input", "instructions", "previous_response_id", "store", "include", "max_output_tokens", "reasoning", "text", "truncation"} {
			keys[key] = true
		}
	}
	return keys
}

func boolPtrValue(value any) *bool {
	typed, ok := value.(bool)
	if !ok {
		return nil
	}
	return &typed
}

func intPtrValue(value any) *int {
	switch typed := value.(type) {
	case int:
		v := typed
		return &v
	case int64:
		v := int(typed)
		return &v
	case float64:
		v := int(typed)
		if float64(v) != typed {
			return nil
		}
		return &v
	case json.Number:
		if i, err := typed.Int64(); err == nil {
			v := int(i)
			return &v
		}
	}
	return nil
}

func int64PtrValue(value any) *int64 {
	switch typed := value.(type) {
	case int:
		v := int64(typed)
		return &v
	case int64:
		v := typed
		return &v
	case float64:
		v := int64(typed)
		if float64(v) != typed {
			return nil
		}
		return &v
	case json.Number:
		if i, err := typed.Int64(); err == nil {
			return &i
		}
	}
	return nil
}

func floatPtrValue(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		v := typed
		return &v
	case float32:
		v := float64(typed)
		return &v
	case int:
		v := float64(typed)
		return &v
	case int64:
		v := float64(typed)
		return &v
	case json.Number:
		if f, err := typed.Float64(); err == nil {
			return &f
		}
	}
	return nil
}

func stringListValue(value any) []string {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	case []string:
		return append([]string(nil), typed...)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := stringValue(item, ""); text != "" {
				items = append(items, text)
			}
		}
		if len(items) == 0 {
			return nil
		}
		return items
	default:
		return nil
	}
}

func mapListValue(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]any)
		if ok {
			out = append(out, cloneAnyMap(record))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
