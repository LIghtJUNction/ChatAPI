package service

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type AutomationObserver struct {
	noRules atomic.Int64
	noMatch atomic.Int64
	mu      sync.Mutex
	skips   map[string]int64
	byRule  map[string]map[string]int64
	recent  []AutomationSkipSample
}

type AutomationObserverSnapshot struct {
	NoRules      int                            `json:"no_rules"`
	NoMatch      int                            `json:"no_match"`
	SkipByReason map[string]int                 `json:"skip_by_reason,omitempty"`
	SkipByRule   map[string]AutomationRuleSkips `json:"skip_by_rule,omitempty"`
	RecentSkips  []AutomationSkipSample         `json:"recent_skips,omitempty"`
}

type AutomationRuleSkips struct {
	Total    int            `json:"total"`
	ByReason map[string]int `json:"by_reason,omitempty"`
}

type AutomationSkipSample struct {
	At             time.Time `json:"at"`
	ConversationID string    `json:"conversation_id,omitempty"`
	RequestFormat  string    `json:"request_format,omitempty"`
	Model          string    `json:"model,omitempty"`
	RuleID         string    `json:"rule_id,omitempty"`
	Reason         string    `json:"reason"`
}

const automationRecentSkipLimit = 100

func NewAutomationObserver() *AutomationObserver {
	return &AutomationObserver{
		skips:  map[string]int64{},
		byRule: map[string]map[string]int64{},
	}
}

func (o *AutomationObserver) RecordNoRules() {
	if o == nil {
		return
	}
	o.noRules.Add(1)
}

func (o *AutomationObserver) RecordNoMatch() {
	if o == nil {
		return
	}
	o.noMatch.Add(1)
}

func (o *AutomationObserver) RecordSkipReason(reason string) {
	if o == nil {
		return
	}
	reason = normalizeAutomationSkipReason(reason)
	if reason == "" {
		return
	}
	o.mu.Lock()
	o.skips[reason]++
	o.mu.Unlock()
}

func (o *AutomationObserver) RecordSkipReasons(reasons []string) {
	for _, reason := range reasons {
		o.RecordSkipReason(reason)
	}
}

func (o *AutomationObserver) RecordSkipDetail(ruleID string, reason string) {
	if o == nil {
		return
	}
	ruleID = strings.TrimSpace(ruleID)
	reason = normalizeAutomationSkipReason(reason)
	if ruleID == "" || reason == "" {
		return
	}
	o.mu.Lock()
	if _, ok := o.byRule[ruleID]; !ok {
		o.byRule[ruleID] = map[string]int64{}
	}
	o.byRule[ruleID][reason]++
	o.mu.Unlock()
}

func (o *AutomationObserver) RecordSkipDetails(details []AutomationRuleSkipDetail) {
	for _, detail := range details {
		o.RecordSkipDetail(detail.RuleID, detail.Reason)
	}
}

func (o *AutomationObserver) RecordSkipSample(sample AutomationSkipSample) {
	if o == nil {
		return
	}
	sample.Reason = normalizeAutomationSkipReason(sample.Reason)
	sample.RuleID = strings.TrimSpace(sample.RuleID)
	sample.ConversationID = strings.TrimSpace(sample.ConversationID)
	sample.RequestFormat = strings.TrimSpace(sample.RequestFormat)
	sample.Model = strings.TrimSpace(sample.Model)
	if sample.Reason == "" {
		return
	}
	if sample.At.IsZero() {
		sample.At = time.Now().UTC()
	}
	o.mu.Lock()
	o.recent = append([]AutomationSkipSample{sample}, o.recent...)
	if len(o.recent) > automationRecentSkipLimit {
		o.recent = o.recent[:automationRecentSkipLimit]
	}
	o.mu.Unlock()
}

func (o *AutomationObserver) RecordSkipSamples(samples []AutomationSkipSample) {
	for _, sample := range samples {
		o.RecordSkipSample(sample)
	}
}

func (o *AutomationObserver) Snapshot() AutomationObserverSnapshot {
	if o == nil {
		return AutomationObserverSnapshot{}
	}
	snapshot := AutomationObserverSnapshot{
		NoRules: int(o.noRules.Load()),
		NoMatch: int(o.noMatch.Load()),
	}
	o.mu.Lock()
	if len(o.skips) > 0 {
		snapshot.SkipByReason = make(map[string]int, len(o.skips))
		keys := make([]string, 0, len(o.skips))
		for key := range o.skips {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			snapshot.SkipByReason[key] = int(o.skips[key])
		}
	}
	if len(o.byRule) > 0 {
		snapshot.SkipByRule = make(map[string]AutomationRuleSkips, len(o.byRule))
		ruleIDs := make([]string, 0, len(o.byRule))
		for ruleID := range o.byRule {
			ruleIDs = append(ruleIDs, ruleID)
		}
		sort.Strings(ruleIDs)
		for _, ruleID := range ruleIDs {
			reasons := o.byRule[ruleID]
			item := AutomationRuleSkips{ByReason: map[string]int{}}
			reasonKeys := make([]string, 0, len(reasons))
			for reason := range reasons {
				reasonKeys = append(reasonKeys, reason)
			}
			sort.Strings(reasonKeys)
			for _, reason := range reasonKeys {
				count := int(reasons[reason])
				item.ByReason[reason] = count
				item.Total += count
			}
			snapshot.SkipByRule[ruleID] = item
		}
	}
	if len(o.recent) > 0 {
		snapshot.RecentSkips = append([]AutomationSkipSample(nil), o.recent...)
	}
	o.mu.Unlock()
	return snapshot
}

func normalizeAutomationSkipReason(value string) string {
	switch value {
	case "parse_invalid", "action_invalid", "empty_output", "contains_miss", "excluded":
		return value
	default:
		return ""
	}
}
