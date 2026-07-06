package service

import "sync/atomic"

type AutomationObserver struct {
	noRules atomic.Int64
	noMatch atomic.Int64
}

type AutomationObserverSnapshot struct {
	NoRules int `json:"no_rules"`
	NoMatch int `json:"no_match"`
}

func NewAutomationObserver() *AutomationObserver {
	return &AutomationObserver{}
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

func (o *AutomationObserver) Snapshot() AutomationObserverSnapshot {
	if o == nil {
		return AutomationObserverSnapshot{}
	}
	return AutomationObserverSnapshot{
		NoRules: int(o.noRules.Load()),
		NoMatch: int(o.noMatch.Load()),
	}
}
