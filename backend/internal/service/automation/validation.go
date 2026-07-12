package automation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	maxRuleSteps         = 128
	maxRegexLength       = 512
	maxDelayMS     int64 = 24 * 60 * 60 * 1000
)

var ErrInvalidRule = errors.New("invalid automation rule")

func NormalizeRule(rule Rule) Rule {
	steps := make([]Step, len(rule.Steps))
	copy(steps, rule.Steps)
	rule.Steps = steps
	rule.ID = strings.TrimSpace(rule.ID)
	rule.OwnerID = strings.TrimSpace(rule.OwnerID)
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Match.Target = "last_user_text"
	rule.Match.Pattern = strings.TrimSpace(rule.Match.Pattern)
	rule.Playback.Mode = strings.TrimSpace(rule.Playback.Mode)
	if rule.Playback.Mode == "" {
		rule.Playback.Mode = "recorded"
	}
	for i := range rule.Steps {
		rule.Steps[i].ID = strings.TrimSpace(rule.Steps[i].ID)
		rule.Steps[i].Action = ActionFromTurn(rule.Steps[i].Action.TurnAction())
	}
	return rule
}

func ValidateRule(rule Rule) error {
	rule = NormalizeRule(rule)
	if rule.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: unsupported schema version", ErrInvalidRule)
	}
	if rule.ID == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidRule)
	}
	if rule.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidRule)
	}
	if len(rule.Match.Pattern) > maxRegexLength {
		return fmt.Errorf("%w: match pattern exceeds %d characters", ErrInvalidRule, maxRegexLength)
	}
	if rule.Enabled && rule.Match.Pattern == "" {
		return fmt.Errorf("%w: enabled rule requires a match pattern", ErrInvalidRule)
	}
	if rule.Match.Pattern != "" {
		if _, err := regexp.Compile(rule.Match.Pattern); err != nil {
			return fmt.Errorf("%w: invalid match pattern: %v", ErrInvalidRule, err)
		}
	}
	if rule.Playback.Mode != "recorded" && rule.Playback.Mode != "fixed" {
		return fmt.Errorf("%w: playback mode must be recorded or fixed", ErrInvalidRule)
	}
	if rule.Playback.InitialDelayMS < 0 || rule.Playback.InitialDelayMS > maxDelayMS ||
		rule.Playback.FixedIntervalMS < 0 || rule.Playback.FixedIntervalMS > maxDelayMS ||
		rule.Playback.LoopIntervalMS < 0 || rule.Playback.LoopIntervalMS > maxDelayMS {
		return fmt.Errorf("%w: playback delay is outside the allowed range", ErrInvalidRule)
	}
	if rule.Playback.Loop && rule.Playback.LoopIntervalMS == 0 {
		return fmt.Errorf("%w: looping rules require a positive loop interval", ErrInvalidRule)
	}
	if len(rule.Steps) > maxRuleSteps {
		return fmt.Errorf("%w: too many steps", ErrInvalidRule)
	}
	if rule.Enabled && len(rule.Steps) == 0 {
		return fmt.Errorf("%w: enabled rule requires at least one step", ErrInvalidRule)
	}
	total := int64(0)
	if rule.Playback.Mode == "fixed" {
		total = rule.Playback.InitialDelayMS
		if len(rule.Steps) > 1 {
			total += int64(len(rule.Steps)-1) * rule.Playback.FixedIntervalMS
		}
	}
	seen := map[string]struct{}{}
	for index, step := range rule.Steps {
		if step.ID == "" {
			return fmt.Errorf("%w: step %d id is required", ErrInvalidRule, index)
		}
		if _, ok := seen[step.ID]; ok {
			return fmt.Errorf("%w: duplicate step id %s", ErrInvalidRule, step.ID)
		}
		seen[step.ID] = struct{}{}
		if step.DelayBeforeMS < 0 || step.DelayBeforeMS > maxDelayMS {
			return fmt.Errorf("%w: step %d delay is outside the allowed range", ErrInvalidRule, index)
		}
		if rule.Playback.Mode == "recorded" {
			total += step.DelayBeforeMS
		}
		if err := step.Action.TurnAction().Validate(); err != nil {
			return fmt.Errorf("%w: step %d: %v", ErrInvalidRule, index, err)
		}
		if !step.Action.Recordable() {
			return fmt.Errorf("%w: step %d references a request-bound image asset", ErrInvalidRule, index)
		}
		if step.Action.Terminal() && index != len(rule.Steps)-1 {
			return fmt.Errorf("%w: terminal action must be the last step", ErrInvalidRule)
		}
		if rule.Playback.Loop && step.Action.Terminal() {
			return fmt.Errorf("%w: looping rules cannot contain terminal actions", ErrInvalidRule)
		}
	}
	if total > maxDelayMS {
		return fmt.Errorf("%w: total recorded playback exceeds 24 hours", ErrInvalidRule)
	}
	return nil
}
