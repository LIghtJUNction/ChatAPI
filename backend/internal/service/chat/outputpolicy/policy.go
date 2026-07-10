package outputpolicy

import (
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/tiktoken-go/tokenizer"
	"github.com/zyf2007/ChatAPI/internal/protocol"
)

type TokenCountAccuracy string

const (
	TokenCountExact     TokenCountAccuracy = "exact"
	TokenCountEstimated TokenCountAccuracy = "estimated"
)

type TokenCounter interface {
	Count(string) (int, error)
	Name() string
}

type TokenCapability struct {
	Counter  TokenCounter
	Accuracy TokenCountAccuracy
}

type AppliedPolicy struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Value        string `json:"value,omitempty"`
	SupportLevel string `json:"support_level"`
}

type Input struct {
	Text  string
	Mode  string
	Final bool
}

type Decision struct {
	Text            string
	OutputText      string
	Terminal        bool
	FinishReason    string
	StopSequence    string
	OutputTokens    int
	TokenLimit      int
	TokenCounter    string
	TokenAccuracy   TokenCountAccuracy
	AppliedPolicies []AppliedPolicy
	pending         string
}

type Guard struct {
	mu         sync.Mutex
	stops      []string
	tokenLimit int
	capability TokenCapability
	text       string
	terminal   bool
	finish     string
	stop       string
	tokens     int
	pending    string
}

func NewGuard(request protocol.TurnRequest) (*Guard, error) {
	limit := tokenLimit(request.Options)
	capability, err := resolveTokenCapability(request.Protocol, request.Model)
	if err != nil {
		return nil, err
	}
	return &Guard{
		stops:      normalizedStops(request.Options.Stop),
		tokenLimit: limit,
		capability: capability,
	}, nil
}

// Execute serializes a policy decision with the persistence operation that
// makes it authoritative. The guard state advances only after persistence.
func (g *Guard) Execute(input Input, persist func(Decision) error) (Decision, error) {
	if g == nil {
		decision := Decision{Text: input.Text, OutputText: input.Text}
		if persist != nil {
			return decision, persist(decision)
		}
		return decision, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	decision, err := g.plan(input)
	if err != nil {
		return Decision{}, err
	}
	if persist != nil {
		if err := persist(decision); err != nil {
			return Decision{}, err
		}
	}
	g.text = decision.OutputText
	g.terminal = decision.Terminal
	g.finish = decision.FinishReason
	g.stop = decision.StopSequence
	g.tokens = decision.OutputTokens
	g.pending = decision.pending
	return decision, nil
}

func (g *Guard) Snapshot() Decision {
	if g == nil {
		return Decision{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.decision("", g.text, g.terminal, g.finish, g.stop, g.tokens)
}

func (g *Guard) plan(input Input) (Decision, error) {
	if g.terminal {
		return g.decision("", g.text, true, g.finish, g.stop, g.tokens), nil
	}
	if normalizedMode(input.Mode) == "tool_result" {
		return g.decision(input.Text, g.text, false, "", "", g.tokens), nil
	}

	combined := g.pending + input.Text
	accepted := combined
	pending := ""
	finishReason := ""
	stopSequence := ""
	if prefix, stop, hit := applyStop(combined, g.stops); hit {
		accepted = prefix
		finishReason = "stop_sequence"
		stopSequence = stop
	} else if !input.Final {
		pending = pendingStopPrefix(combined, g.stops)
		accepted = strings.TrimSuffix(combined, pending)
	}

	outputText := g.text + accepted
	tokens := g.tokens
	if g.capability.Counter != nil {
		var err error
		tokens, err = g.capability.Counter.Count(outputText)
		if err != nil {
			return Decision{}, err
		}
		if g.tokenLimit > 0 && tokens > g.tokenLimit {
			accepted, outputText, tokens, err = g.truncateToBudget(accepted)
			if err != nil {
				return Decision{}, err
			}
			finishReason = "length"
			stopSequence = ""
			pending = ""
		}
	}

	decision := g.decision(
		accepted,
		outputText,
		finishReason != "",
		finishReason,
		stopSequence,
		tokens,
	)
	decision.pending = pending
	return decision, nil
}

func (g *Guard) truncateToBudget(input string) (accepted string, output string, tokens int, err error) {
	runes := []rune(input)
	low, high := 0, len(runes)
	for low < high {
		mid := (low + high + 1) / 2
		candidate := g.text + string(runes[:mid])
		count, countErr := g.capability.Counter.Count(candidate)
		if countErr != nil {
			return "", "", 0, countErr
		}
		if count <= g.tokenLimit {
			low = mid
		} else {
			high = mid - 1
		}
	}
	accepted = string(runes[:low])
	output = g.text + accepted
	tokens, err = g.capability.Counter.Count(output)
	return accepted, output, tokens, err
}

func (g *Guard) decision(accepted string, output string, terminal bool, finish string, stop string, tokens int) Decision {
	decision := Decision{
		Text:          accepted,
		OutputText:    output,
		Terminal:      terminal,
		FinishReason:  finish,
		StopSequence:  stop,
		OutputTokens:  tokens,
		TokenLimit:    g.tokenLimit,
		TokenAccuracy: g.capability.Accuracy,
	}
	if g.capability.Counter != nil {
		decision.TokenCounter = g.capability.Counter.Name()
	}
	if g.tokenLimit > 0 {
		decision.AppliedPolicies = append(decision.AppliedPolicies, AppliedPolicy{
			Key:          "token_budget",
			Label:        "tokens",
			Value:        strconv.Itoa(tokens) + "/" + strconv.Itoa(g.tokenLimit),
			SupportLevel: "applied",
		})
	}
	if finish == "stop_sequence" {
		decision.AppliedPolicies = append(decision.AppliedPolicies, AppliedPolicy{
			Key:          "stop_hit",
			Label:        "stop hit",
			Value:        stop,
			SupportLevel: "applied",
		})
	}
	if finish == "length" {
		decision.AppliedPolicies = append(decision.AppliedPolicies, AppliedPolicy{
			Key:          "token_limit_hit",
			Label:        "token limit",
			Value:        strconv.Itoa(g.tokenLimit),
			SupportLevel: "applied",
		})
	}
	return decision
}

func (d Decision) Metadata() map[string]any {
	if d.TokenCounter == "" && d.FinishReason == "" && len(d.AppliedPolicies) == 0 {
		return nil
	}
	out := map[string]any{}
	if d.FinishReason != "" {
		out["finish_reason"] = d.FinishReason
	}
	if d.StopSequence != "" {
		out["stop_sequence"] = d.StopSequence
	}
	if d.TokenCounter != "" {
		out["output_tokens"] = d.OutputTokens
		out["token_counter"] = d.TokenCounter
		out["token_count_accuracy"] = string(d.TokenAccuracy)
	}
	if d.TokenLimit > 0 {
		out["token_limit"] = d.TokenLimit
	}
	if len(d.AppliedPolicies) > 0 {
		out["applied_chips"] = d.AppliedPolicies
	}
	return out
}

type codecCounter struct {
	codec tokenizer.Codec
}

func (c codecCounter) Count(text string) (int, error) { return c.codec.Count(text) }
func (c codecCounter) Name() string                   { return c.codec.GetName() }

func resolveTokenCapability(proto protocol.Protocol, model string) (TokenCapability, error) {
	model = strings.TrimSpace(model)
	if proto != protocol.ProtocolAnthropicMessages && model != "" {
		if codec, err := tokenizer.ForModel(tokenizer.Model(model)); err == nil {
			return TokenCapability{Counter: codecCounter{codec: codec}, Accuracy: TokenCountExact}, nil
		}
	}
	encoding := tokenizer.O200kBase
	if proto == protocol.ProtocolAnthropicMessages {
		encoding = tokenizer.Cl100kBase
	}
	codec, err := tokenizer.Get(encoding)
	if err != nil {
		return TokenCapability{}, errors.New("token counter unavailable: " + err.Error())
	}
	return TokenCapability{Counter: codecCounter{codec: codec}, Accuracy: TokenCountEstimated}, nil
}

func applyStop(candidate string, stops []string) (string, string, bool) {
	bestIndex := -1
	bestStop := ""
	for _, stop := range stops {
		index := strings.Index(candidate, stop)
		if index < 0 {
			continue
		}
		if bestIndex < 0 || index < bestIndex {
			bestIndex = index
			bestStop = stop
		}
	}
	if bestIndex < 0 {
		return candidate, "", false
	}
	return candidate[:bestIndex], bestStop, true
}

func pendingStopPrefix(text string, stops []string) string {
	best := ""
	for _, stop := range stops {
		stopRunes := []rune(stop)
		for length := len(stopRunes) - 1; length > 0; length-- {
			prefix := string(stopRunes[:length])
			if len(prefix) <= len(best) {
				break
			}
			if strings.HasSuffix(text, prefix) {
				best = prefix
				break
			}
		}
	}
	return best
}

func tokenLimit(options protocol.TurnOptions) int {
	for _, value := range []*int{options.MaxOutputTokens, options.MaxCompletionTokens, options.MaxTokens} {
		if value != nil && *value > 0 {
			return *value
		}
	}
	return 0
}

func normalizedStops(stops []string) []string {
	out := make([]string, 0, len(stops))
	for _, stop := range stops {
		if stop != "" {
			out = append(out, stop)
		}
	}
	return out
}

func normalizedMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "tool_call":
		return "tool_call"
	case "tool_result":
		return "tool_result"
	case "thinking":
		return "thinking"
	default:
		return "assistant_message"
	}
}
