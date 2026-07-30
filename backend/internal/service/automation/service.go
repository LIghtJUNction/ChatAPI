package automation

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	automationrepo "github.com/zyf2007/ChatAPI/internal/repository/automation"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	automationsettings "github.com/zyf2007/ChatAPI/internal/service/automation/settings"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	conversationstate "github.com/zyf2007/ChatAPI/internal/service/chat/conversationstate"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	"go.uber.org/zap"
)

var (
	ErrRecordingNotFound = errors.New("automation recording not found")
	ErrRecordingConflict = errors.New("automation recording already active")
	ErrPendingNotFound   = errors.New("active pending turn not found")
)

type ControlExecutor interface {
	Execute(context.Context, controlsvc.Command) (controlsvc.Result, error)
	Synchronize(context.Context, string, func() error) error
}

type PendingLookup interface {
	GetByConversationID(string) (*turnsvc.PendingTurn, bool)
}

type ModelKeyStore interface {
	ListModelAPIKeysByUser(context.Context, string) ([]common.ModelAPIKey, error)
}

type Deps struct {
	Rules     automationrepo.Store
	ModelKeys ModelKeyStore
	Control   ControlExecutor
	Pending   PendingLookup
	Events    StatePublisher
	Logger    *zap.Logger
	Settings  *automationsettings.Service
}

type Service struct {
	rules     automationrepo.Store
	modelKeys ModelKeyStore
	control   ControlExecutor
	pending   PendingLookup
	events    StatePublisher
	logger    *zap.Logger
	settings  *automationsettings.Service

	mu              sync.Mutex
	recordings      map[string]*recording
	executions      map[string]*execution
	manualTakeovers map[string]struct{}
	ruleGenerations map[string]uint64
	ownerRevisions  map[string]uint64
}

func New(deps Deps) *Service {
	return &Service{
		rules: deps.Rules, modelKeys: deps.ModelKeys, control: deps.Control, pending: deps.Pending,
		events: deps.Events, logger: deps.Logger, settings: deps.Settings,
		recordings: map[string]*recording{}, executions: map[string]*execution{},
		manualTakeovers: map[string]struct{}{}, ruleGenerations: map[string]uint64{}, ownerRevisions: map[string]uint64{},
	}
}

func (s *Service) SetControl(control ControlExecutor) { s.control = control }
func (s *Service) ListRules(ctx context.Context, ownerID string) ([]Rule, error) {
	rows, err := s.rules.ListAutomationRulesByUser(ctx, strings.TrimSpace(ownerID))
	if err != nil {
		return nil, err
	}
	items := make([]Rule, 0, len(rows))
	var migrationKeyID string
	var migrationKeyLoaded bool
	for _, row := range rows {
		rule, err := ruleFromStored(row)
		if err != nil {
			continue
		}
		if rule.SchemaVersion == 2 {
			if s.modelKeys == nil {
				continue
			}
			if !migrationKeyLoaded {
				migrationKeyID, err = s.firstActiveModelKeyID(ctx, strings.TrimSpace(ownerID))
				if err != nil {
					return nil, err
				}
				migrationKeyLoaded = true
			}
			rule, err = s.migrateV2Rule(ctx, rule, migrationKeyID)
			if err != nil {
				return nil, err
			}
		}
		if rule.SchemaVersion != SchemaVersion || ValidateRule(rule) != nil {
			continue
		}
		items = append(items, rule)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (s *Service) firstActiveModelKeyID(ctx context.Context, ownerID string) (string, error) {
	keys, err := s.modelKeys.ListModelAPIKeysByUser(ctx, ownerID)
	if err != nil {
		return "", err
	}
	for _, key := range keys {
		if key.RevokedAt == nil && strings.TrimSpace(key.ID) != "" {
			return strings.TrimSpace(key.ID), nil
		}
	}
	return "", nil
}

func (s *Service) migrateV2Rule(ctx context.Context, rule Rule, modelKeyID string) (Rule, error) {
	rule.SchemaVersion = SchemaVersion
	rule.Match = MatchSpec{
		Pattern: rule.Match.Pattern, ModelPattern: ".*", ModelKeyID: strings.TrimSpace(modelKeyID),
	}
	if rule.Match.ModelKeyID == "" {
		rule.Enabled = false
	}
	payload, err := rulePayload(rule)
	if err != nil {
		return Rule{}, err
	}
	stored, err := s.rules.UpsertAutomationRule(ctx, common.UpsertAutomationRuleInput{
		ID: rule.ID, UserID: rule.OwnerID, Enabled: rule.Enabled, Payload: payload,
	})
	if err != nil {
		return Rule{}, err
	}
	return ruleFromStored(stored)
}

func (s *Service) SaveRule(ctx context.Context, ownerID string, rule Rule) (Rule, error) {
	ownerID = strings.TrimSpace(ownerID)
	if strings.TrimSpace(rule.ID) == "" {
		rule.ID = "rule_" + uuid.NewString()
	}
	rule.OwnerID = ownerID
	rule = NormalizeRule(rule)
	if err := ValidateRule(rule); err != nil {
		return Rule{}, err
	}
	if s.settings != nil {
		cfg, err := s.settings.Current(ctx)
		if err != nil {
			return Rule{}, err
		}
		if len(rule.Steps) > cfg.MaxSteps {
			return Rule{}, errors.New("automation rule exceeds global step limit")
		}
		if rule.Playback.LoopIntervalMS > cfg.MaxLoopIntervalMS {
			return Rule{}, errors.New("automation loop interval exceeds global limit")
		}
	}
	payload, err := rulePayload(rule)
	if err != nil {
		return Rule{}, err
	}
	stored, err := s.rules.UpsertAutomationRule(ctx, common.UpsertAutomationRuleInput{
		ID: rule.ID, UserID: ownerID, Enabled: rule.Enabled, Payload: payload,
	})
	if err != nil {
		return Rule{}, err
	}
	s.advanceRuleGeneration(ownerID)
	result, err := ruleFromStored(stored)
	if err == nil && !result.Enabled {
		s.cancelRuleExecutions(result.OwnerID, result.ID, "rule_disabled")
	}
	return result, err
}

func (s *Service) DeleteRule(ctx context.Context, ownerID string, ruleID string) error {
	if err := s.rules.DeleteAutomationRule(ctx, strings.TrimSpace(ownerID), strings.TrimSpace(ruleID)); err != nil {
		return err
	}
	s.advanceRuleGeneration(strings.TrimSpace(ownerID))
	s.cancelRuleExecutions(strings.TrimSpace(ownerID), strings.TrimSpace(ruleID), "rule_deleted")
	return nil
}

func rulePayload(rule Rule) (map[string]any, error) {
	copy := rule
	copy.ID = ""
	copy.OwnerID = ""
	copy.Enabled = false
	copy.CreatedAt = time.Time{}
	copy.UpdatedAt = time.Time{}
	data, err := json.Marshal(copy)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func ruleFromStored(stored common.AutomationRule) (Rule, error) {
	data, err := json.Marshal(stored.Payload)
	if err != nil {
		return Rule{}, err
	}
	var rule Rule
	if err := json.Unmarshal(data, &rule); err != nil {
		return Rule{}, err
	}
	rule.ID = stored.ID
	rule.OwnerID = stored.UserID
	rule.Enabled = stored.Enabled
	rule.CreatedAt = stored.CreatedAt
	rule.UpdatedAt = stored.UpdatedAt
	return NormalizeRule(rule), nil
}

func cloneRule(rule Rule) Rule {
	cloned := rule
	cloned.Steps = make([]Step, len(rule.Steps))
	copy(cloned.Steps, rule.Steps)
	return cloned
}

func (s *Service) ExecutionStates(ownerID string) []ExecutionState {
	return s.StateSnapshot(ownerID).Executions
}

func (s *Service) StateSnapshot(ownerID string) StateSnapshot {
	ownerID = strings.TrimSpace(ownerID)
	s.mu.Lock()
	defer s.mu.Unlock()
	revision := s.ownerRevisions[ownerID]
	recording := RecordingState{Revision: revision, Steps: []Step{}}
	if current := s.recordings[ownerID]; current != nil {
		recording = cloneRecordingState(current.state)
	}
	items := make([]ExecutionState, 0)
	for _, current := range s.executions {
		if current != nil && current.state.OwnerID == ownerID {
			items = append(items, current.state)
		}
	}
	return StateSnapshot{Revision: revision, Recording: recording, Executions: items}
}

func (s *Service) nextRevisionLocked(ownerID string) uint64 {
	ownerID = strings.TrimSpace(ownerID)
	s.ownerRevisions[ownerID]++
	return s.ownerRevisions[ownerID]
}

func (s *Service) HandleChatEvent(ctx context.Context, event chatevents.Event) {
	if event.Type == chatevents.TypeTurnWaiting && event.WaitingTurn != nil {
		waiting := *event.WaitingTurn
		go s.matchAndRun(context.WithoutCancel(ctx), waiting)
		return
	}
	if event.Type == chatevents.TypeConversationUpserted && !conversationstate.IsPendingStatus(conversationstate.FromConversation(event.Conversation).Status) {
		s.cancelRequestExecution(event.ConversationID, event.RequestID, "pending_ended")
		matched := s.markRecordingTerminated(event.OwnerID, event.ConversationID, event.RequestID)
		if matched && !event.ControlManaged {
			go func() { _, _ = s.StopRecording(context.Background(), event.OwnerID) }()
		}
		s.clearManualTakeover(event.ConversationID, event.RequestID)
	}
}
