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
	if s.settings != nil {
		cfg, err := s.settings.Current(ctx)
		if err != nil || !cfg.Enabled {
			return
		}
	}
	generation := s.ruleGeneration(waiting.OwnerID)
	rules, err := s.ListRules(ctx, waiting.OwnerID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("automation rules load failed", zap.Error(err), zap.String("owner.id", waiting.OwnerID))
		}
		return
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !matchesWaitingTurn(rule.Match, waiting) {
			continue
		}
		s.startExecutionAtGeneration(ctx, waiting, rule, generation)
		return
	}
}

func matchesWaitingTurn(spec MatchSpec, waiting chatevents.WaitingTurn) bool {
	textMatcher, err := regexp.Compile(strings.TrimSpace(spec.Pattern))
	if err != nil || !textMatcher.MatchString(waiting.LastUserText) {
		return false
	}
	modelMatcher, err := regexp.Compile(strings.TrimSpace(spec.ModelPattern))
	return err == nil &&
		modelMatcher.MatchString(strings.TrimSpace(waiting.Model)) &&
		strings.TrimSpace(spec.ModelKeyID) != "" &&
		strings.TrimSpace(spec.ModelKeyID) == strings.TrimSpace(waiting.ModelKeyID)
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
		if err := s.validateExecutionSettings(ctx, rule); err != nil {
			s.finishExecution(waiting.OwnerID, current, state, "cancelled", err.Error())
			return
		}
		state.Cycle = cycle
		for index, step := range rule.Steps {
			if err := s.validateExecutionSettings(ctx, rule); err != nil {
				s.finishExecution(waiting.OwnerID, current, state, "cancelled", err.Error())
				return
			}
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
			if waitErr := s.waitForStep(ctx, time.Duration(delay)*time.Millisecond, rule); waitErr != nil {
				reason := "cancelled"
				if cause := context.Cause(ctx); cause != nil {
					reason = cause.Error()
				} else {
					reason = waitErr.Error()
				}
				s.finishExecution(waiting.OwnerID, current, state, "cancelled", reason)
				return
			}
			if err := s.validateExecutionSettings(ctx, rule); err != nil {
				s.finishExecution(waiting.OwnerID, current, state, "cancelled", err.Error())
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

func (s *Service) validateExecutionSettings(ctx context.Context, rule Rule) error {
	if s.settings == nil {
		return nil
	}
	current, err := s.settings.Current(ctx)
	if err != nil {
		return err
	}
	if !current.Enabled {
		return errors.New("automation_disabled")
	}
	if len(rule.Steps) > current.MaxSteps {
		return errors.New("automation_step_limit_changed")
	}
	if rule.Playback.LoopIntervalMS > current.MaxLoopIntervalMS {
		return errors.New("automation_loop_limit_changed")
	}
	return nil
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

func (s *Service) waitForStep(ctx context.Context, delay time.Duration, rule Rule) error {
	if delay <= 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		return s.validateExecutionSettings(ctx, rule)
	}
	deadline := time.NewTimer(delay)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return s.validateExecutionSettings(ctx, rule)
		case <-ticker.C:
			if err := s.validateExecutionSettings(ctx, rule); err != nil {
				return err
			}
		}
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
