package automation

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	"go.uber.org/zap"
)

type execution struct {
	state      ExecutionState
	cancel     context.CancelCauseFunc
	finishedAt time.Time
}

const executionStateRetention = 10 * time.Minute

func (s *Service) matchAndRun(ctx context.Context, waiting chatevents.WaitingTurn) {
	generation := s.ruleGeneration(waiting.OwnerID)
	rules, err := s.ListRules(ctx, waiting.OwnerID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("automation rules load failed", zap.Error(err), zap.String("owner.id", waiting.OwnerID))
		}
		return
	}
	for _, rule := range rules {
		if !rule.Enabled || rule.Match.Pattern == "" {
			continue
		}
		matcher, err := regexp.Compile(rule.Match.Pattern)
		if err != nil || !matcher.MatchString(waiting.LastUserText) {
			continue
		}
		s.startExecutionAtGeneration(ctx, waiting, rule, generation)
		return
	}
}

func (s *Service) startExecution(parent context.Context, waiting chatevents.WaitingTurn, rule Rule) {
	s.startExecutionAtGeneration(parent, waiting, rule, s.ruleGeneration(waiting.OwnerID))
}

func (s *Service) startExecutionAtGeneration(parent context.Context, waiting chatevents.WaitingTurn, rule Rule, generation uint64) bool {
	ctx, cancel := context.WithCancelCause(parent)
	key := strings.TrimSpace(waiting.ConversationID)
	pendingTurn, ok := s.pending.GetByConversationID(key)
	if !ok || pendingTurn == nil {
		cancel(errors.New("pending_changed"))
		return false
	}
	if pendingTurn.MutationMu != nil {
		pendingTurn.MutationMu.Lock()
		defer pendingTurn.MutationMu.Unlock()
	}
	currentTurn, ok := s.pending.GetByConversationID(key)
	if !ok || currentTurn == nil ||
		strings.TrimSpace(currentTurn.OwnerID) != strings.TrimSpace(waiting.OwnerID) ||
		strings.TrimSpace(currentTurn.RequestID) != strings.TrimSpace(waiting.RequestID) {
		cancel(errors.New("pending_changed"))
		return false
	}
	state := ExecutionState{
		OwnerID: waiting.OwnerID, RuleID: rule.ID, ConversationID: key, RequestID: waiting.RequestID,
		Status: "running", StepCount: len(rule.Steps), Cycle: 1,
	}
	current := &execution{cancel: cancel}
	s.mu.Lock()
	if s.ruleGenerations[strings.TrimSpace(waiting.OwnerID)] != generation {
		s.mu.Unlock()
		cancel(errors.New("rule_generation_changed"))
		return false
	}
	if _, blocked := s.manualTakeovers[executionAuthorityKey(waiting.ConversationID, waiting.RequestID)]; blocked {
		s.mu.Unlock()
		cancel(errors.New("manual_takeover"))
		return false
	}
	if previous := s.executions[key]; previous != nil && previous.state.Status == "running" {
		previous.cancel(errors.New("replaced_by_new_execution"))
	}
	state.Revision = s.nextRevisionLocked(waiting.OwnerID)
	current.state = state
	s.executions[key] = current
	s.mu.Unlock()
	s.publishExecution(waiting.OwnerID, state)
	go s.runExecution(ctx, waiting, rule, current)
	return true
}

func (s *Service) runExecution(ctx context.Context, waiting chatevents.WaitingTurn, rule Rule, current *execution) {
	state := ExecutionState{OwnerID: waiting.OwnerID, RuleID: rule.ID, ConversationID: waiting.ConversationID, RequestID: waiting.RequestID, Status: "running", StepCount: len(rule.Steps), Cycle: 1}
	for cycle := 1; ; cycle++ {
		state.Cycle = cycle
		for index, step := range rule.Steps {
			state.StepIndex = index
			delay := step.DelayBeforeMS
			if cycle > 1 && index == 0 {
				delay = rule.Playback.LoopIntervalMS
			} else if rule.Playback.Mode == "fixed" {
				if index == 0 {
					delay = rule.Playback.InitialDelayMS
				} else {
					delay = rule.Playback.FixedIntervalMS
				}
			}
			if !waitContext(ctx, time.Duration(delay)*time.Millisecond) {
				reason := "cancelled"
				if cause := context.Cause(ctx); cause != nil {
					reason = cause.Error()
				}
				s.finishExecution(waiting.OwnerID, current, state, "cancelled", reason)
				return
			}
			turn, ok := s.pending.GetByConversationID(waiting.ConversationID)
			if !ok || turn == nil || strings.TrimSpace(turn.RequestID) != strings.TrimSpace(waiting.RequestID) {
				s.finishExecution(waiting.OwnerID, current, state, "cancelled", "pending_changed")
				return
			}
			if s.control == nil {
				s.finishExecution(waiting.OwnerID, current, state, "failed", "control_unavailable")
				return
			}
			_, err := s.control.Execute(ctx, controlsvc.Command{
				OwnerID: waiting.OwnerID, ConversationID: waiting.ConversationID,
				RequestID: waiting.RequestID, ResponseID: waiting.ResponseID, Source: controlsvc.SourceAutomation,
				Action: step.Action.TurnAction(),
			})
			if err != nil {
				if ctx.Err() != nil {
					reason := "cancelled"
					if cause := context.Cause(ctx); cause != nil {
						reason = cause.Error()
					}
					s.finishExecution(waiting.OwnerID, current, state, "cancelled", reason)
					return
				}
				s.finishExecution(waiting.OwnerID, current, state, "failed", err.Error())
				return
			}
			state.StepIndex = index + 1
			if s.updateExecutionState(current, state) {
				s.publishExecution(waiting.OwnerID, state)
			}
		}
		if !rule.Playback.Loop {
			break
		}
	}
	s.finishExecution(waiting.OwnerID, current, state, "completed", "")
}

func (s *Service) updateExecutionState(current *execution, state ExecutionState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.executions[state.ConversationID] != current {
		return false
	}
	state.Revision = s.nextRevisionLocked(state.OwnerID)
	current.state = state
	return true
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Service) finishExecution(ownerID string, current *execution, state ExecutionState, status string, reason string) {
	state.Status, state.Reason = status, reason
	s.mu.Lock()
	if s.executions[state.ConversationID] != current {
		s.mu.Unlock()
		return
	}
	state.Revision = s.nextRevisionLocked(ownerID)
	current.state = state
	current.finishedAt = time.Now().UTC()
	s.mu.Unlock()
	s.publishExecution(ownerID, state)
	time.AfterFunc(executionStateRetention, func() {
		s.expireExecution(ownerID, state.ConversationID, current, time.Now().UTC())
	})
}

func (s *Service) expireExecution(ownerID string, conversationID string, expected *execution, now time.Time) bool {
	s.mu.Lock()
	if s.executions[conversationID] != expected || expected.state.Status == "running" || now.Sub(expected.finishedAt) < executionStateRetention {
		s.mu.Unlock()
		return false
	}
	removed := expected.state
	removed.Status = "removed"
	removed.Reason = "retention_expired"
	removed.Revision = s.nextRevisionLocked(ownerID)
	delete(s.executions, conversationID)
	s.mu.Unlock()
	s.publishExecution(ownerID, removed)
	return true
}

func (s *Service) cancelRuleExecutions(ownerID string, ruleID string, reason string) {
	s.mu.Lock()
	for _, current := range s.executions {
		if current.state.Status == "running" && current.state.OwnerID == ownerID && current.state.RuleID == ruleID {
			current.state.Reason = reason
			current.cancel(errors.New(reason))
		}
	}
	s.mu.Unlock()
}

func (s *Service) ruleGeneration(ownerID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ruleGenerations[strings.TrimSpace(ownerID)]
}

func (s *Service) advanceRuleGeneration(ownerID string) {
	s.mu.Lock()
	s.ruleGenerations[strings.TrimSpace(ownerID)]++
	s.mu.Unlock()
}

func executionAuthorityKey(conversationID string, requestID string) string {
	return strings.TrimSpace(conversationID) + "\x00" + strings.TrimSpace(requestID)
}

func (s *Service) markManualTakeover(conversationID string, requestID string) {
	conversationID = strings.TrimSpace(conversationID)
	requestID = strings.TrimSpace(requestID)
	if conversationID == "" || requestID == "" {
		return
	}
	s.mu.Lock()
	s.manualTakeovers[executionAuthorityKey(conversationID, requestID)] = struct{}{}
	if current := s.executions[conversationID]; current != nil && current.state.Status == "running" && current.state.RequestID == requestID {
		current.state.Reason = "manual_control"
		current.cancel(errors.New("manual_control"))
	}
	s.mu.Unlock()
}

func (s *Service) cancelRequestExecution(conversationID string, requestID string, reason string) {
	conversationID = strings.TrimSpace(conversationID)
	requestID = strings.TrimSpace(requestID)
	s.mu.Lock()
	if current := s.executions[conversationID]; current != nil && current.state.Status == "running" && current.state.RequestID == requestID {
		current.state.Reason = reason
		current.cancel(errors.New(reason))
	}
	s.mu.Unlock()
}

func (s *Service) clearManualTakeover(conversationID string, requestID string) {
	s.mu.Lock()
	delete(s.manualTakeovers, executionAuthorityKey(conversationID, requestID))
	s.mu.Unlock()
}

func (s *Service) publishExecution(ownerID string, state ExecutionState) {
	if s.events != nil {
		copy := state
		s.events.PublishAutomationState(context.Background(), StateEvent{OwnerID: ownerID, Execution: &copy})
	}
}
