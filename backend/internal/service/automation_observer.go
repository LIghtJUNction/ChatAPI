package service

import (
	"sort"
	"sync"
	"sync/atomic"
)

type AutomationObserver struct {
	noRules atomic.Int64
	noMatch atomic.Int64
	mu      sync.Mutex
	skips   map[string]int64
}

type AutomationObserverSnapshot struct {
	NoRules      int            `json:"no_rules"`
	NoMatch      int            `json:"no_match"`
	SkipByReason map[string]int `json:"skip_by_reason,omitempty"`
}

func NewAutomationObserver() *AutomationObserver {
	return &AutomationObserver{skips: map[string]int64{}}
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
