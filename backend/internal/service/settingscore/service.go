package settingscore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type Store interface {
	GetSystemConfig(context.Context, string) (common.SystemConfig, error)
	SetSystemConfig(context.Context, common.SetSystemConfigInput) (common.SystemConfig, error)
}

type BatchStore interface {
	SetSystemConfigs(context.Context, []common.SetSystemConfigInput) ([]common.SystemConfig, error)
}

type PreparedPatch struct {
	service *Service
	input   common.SetSystemConfigInput
	restart []string
}

type Level string
type Source string

const (
	LevelCommon       Level  = "common"
	LevelPolicy       Level  = "policy"
	LevelAdvanced     Level  = "advanced"
	LevelStartup      Level  = "startup"
	SourceDefault     Source = "default"
	SourceDatabase    Source = "database"
	SourceEnvironment Source = "environment"
)

type Descriptor struct {
	Key             string         `json:"key"`
	Type            string         `json:"type"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Level           Level          `json:"level"`
	Editable        bool           `json:"editable"`
	Sensitive       bool           `json:"sensitive"`
	RestartRequired bool           `json:"restart_required"`
	Default         any            `json:"default"`
	Minimum         *float64       `json:"minimum,omitempty"`
	Maximum         *float64       `json:"maximum,omitempty"`
	Enum            []string       `json:"enum,omitempty"`
	Unit            string         `json:"unit,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type Document struct {
	Domain       string            `json:"domain"`
	Title        string            `json:"title"`
	UpdatedAt    time.Time         `json:"updated_at,omitempty"`
	Values       map[string]any    `json:"values"`
	Sources      map[string]Source `json:"sources"`
	Fields       []Descriptor      `json:"fields"`
	Stale        bool              `json:"stale,omitempty"`
	RefreshError string            `json:"refresh_error,omitempty"`
}

type Spec struct {
	Domain      string
	Title       string
	StorageKey  string
	Fields      []Descriptor
	Defaults    map[string]any
	Environment map[string]any
	Validate    func(map[string]any) error
}

type Service struct {
	store   Store
	spec    Spec
	mu      sync.RWMutex
	loadMu  sync.Mutex
	cache   *Document
	cacheAt time.Time
}

func New(store Store, spec Spec) *Service {
	s := &Service{store: store, spec: spec}
	if doc, err := s.document(nil); err == nil {
		s.cache = &doc
	}
	return s
}
func (s *Service) Domain() string       { return s.spec.Domain }
func (s *Service) Title() string        { return s.spec.Title }
func (s *Service) Fields() []Descriptor { return append([]Descriptor(nil), s.spec.Fields...) }

func (s *Service) Get(ctx context.Context) (Document, error) {
	if s == nil || s.store == nil {
		return Document{}, errors.New("settings store unavailable")
	}
	s.mu.RLock()
	if s.cache != nil && time.Since(s.cacheAt) < 2*time.Second {
		out := cloneDocument(*s.cache)
		s.mu.RUnlock()
		return out, nil
	}
	s.mu.RUnlock()
	doc, err := s.Reload(ctx)
	if err == nil {
		return doc, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache != nil {
		fallback := cloneDocument(*s.cache)
		fallback.Stale = true
		fallback.RefreshError = err.Error()
		return fallback, nil
	}
	return Document{}, err
}

func (s *Service) Reload(ctx context.Context) (Document, error) {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	return s.reload(ctx)
}

func (s *Service) reload(ctx context.Context) (Document, error) {
	stored, err := s.loadStored(ctx)
	if err != nil {
		return Document{}, err
	}
	doc, err := s.document(stored)
	if err != nil {
		return Document{}, err
	}
	s.storeCache(doc)
	return cloneDocument(doc), nil
}

func (s *Service) loadStored(ctx context.Context) (*common.SystemConfig, error) {
	item, err := s.store.GetSystemConfig(ctx, s.spec.StorageKey)
	if errors.Is(err, common.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) document(item *common.SystemConfig) (Document, error) {
	values := cloneMap(s.spec.Defaults)
	sources := map[string]Source{}
	for key := range values {
		sources[key] = SourceDefault
	}
	var updatedAt time.Time
	if item != nil {
		updatedAt = item.UpdatedAt
		for key, value := range item.Value {
			if s.hasField(key) {
				values[key], sources[key] = value, SourceDatabase
			}
		}
	}
	for key, value := range s.spec.Environment {
		values[key], sources[key] = value, SourceEnvironment
	}
	if err := s.validateFieldValues(values); err != nil {
		return Document{}, err
	}
	if s.spec.Validate != nil {
		if err := s.spec.Validate(values); err != nil {
			return Document{}, err
		}
	}
	return Document{Domain: s.spec.Domain, Title: s.spec.Title, UpdatedAt: updatedAt, Values: values, Sources: sources, Fields: s.Fields()}, nil
}

func (s *Service) Patch(ctx context.Context, patch map[string]any) (Document, []string, error) {
	prepared, err := s.PreparePatch(ctx, patch)
	if err != nil {
		return Document{}, nil, err
	}
	documents, restart, err := CommitPreparedPatches(ctx, []PreparedPatch{prepared})
	if err != nil {
		return Document{}, nil, err
	}
	return documents[0], restart, nil
}

func (s *Service) PreparePatch(ctx context.Context, patch map[string]any) (PreparedPatch, error) {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	stored, err := s.loadStored(ctx)
	if err != nil {
		return PreparedPatch{}, err
	}
	current, err := s.document(stored)
	if err != nil {
		return PreparedPatch{}, err
	}
	next := cloneMap(current.Values)
	for key, value := range patch {
		field, ok := s.field(key)
		if !ok {
			return PreparedPatch{}, fmt.Errorf("unknown setting %q", key)
		}
		if !field.Editable || current.Sources[key] == SourceEnvironment {
			return PreparedPatch{}, fmt.Errorf("setting %q is not editable", key)
		}
		if err := validateFieldValue(field, value); err != nil {
			return PreparedPatch{}, err
		}
		next[key] = value
	}
	if err := s.validateFieldValues(next); err != nil {
		return PreparedPatch{}, err
	}
	if s.spec.Validate != nil {
		if err := s.spec.Validate(next); err != nil {
			return PreparedPatch{}, err
		}
	}
	persisted := map[string]any{}
	if stored != nil {
		persisted = cloneMap(stored.Value)
	}
	for key, value := range patch {
		persisted[key] = value
	}
	restart := make([]string, 0)
	for key := range patch {
		field, _ := s.field(key)
		if field.RestartRequired {
			restart = append(restart, key)
		}
	}
	return PreparedPatch{service: s, input: common.SetSystemConfigInput{Key: s.spec.StorageKey, Value: persisted}, restart: restart}, nil
}

func CommitPreparedPatches(ctx context.Context, patches []PreparedPatch) ([]Document, []string, error) {
	if len(patches) == 0 {
		return nil, nil, nil
	}
	store := patches[0].service.store
	inputs := make([]common.SetSystemConfigInput, len(patches))
	for index, patch := range patches {
		if patch.service == nil || patch.service.store != store {
			return nil, nil, errors.New("combined settings must share one configuration store")
		}
		inputs[index] = patch.input
	}
	var items []common.SystemConfig
	if batch, ok := store.(BatchStore); ok {
		var err error
		items, err = batch.SetSystemConfigs(ctx, inputs)
		if err != nil {
			return nil, nil, err
		}
	} else if len(inputs) == 1 {
		item, err := store.SetSystemConfig(ctx, inputs[0])
		if err != nil {
			return nil, nil, err
		}
		items = []common.SystemConfig{item}
	} else {
		return nil, nil, errors.New("configuration store does not support atomic batch updates")
	}
	if len(items) != len(patches) {
		return nil, nil, errors.New("configuration batch returned an unexpected item count")
	}
	documents := make([]Document, len(patches))
	restart := make([]string, 0)
	for index, patch := range patches {
		document, err := patch.service.document(&items[index])
		if err != nil {
			return nil, nil, err
		}
		patch.service.storeCache(document)
		documents[index] = cloneDocument(document)
		restart = append(restart, patch.restart...)
	}
	return documents, restart, nil
}

func (s *Service) ValidatePatch(ctx context.Context, patch map[string]any) error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	stored, err := s.loadStored(ctx)
	if err != nil {
		return err
	}
	current, err := s.document(stored)
	if err != nil {
		return err
	}
	next := cloneMap(current.Values)
	for key, value := range patch {
		field, ok := s.field(key)
		if !ok {
			return fmt.Errorf("unknown setting %q", key)
		}
		if !field.Editable || current.Sources[key] == SourceEnvironment {
			return fmt.Errorf("setting %q is not editable", key)
		}
		if err := validateFieldValue(field, value); err != nil {
			return err
		}
		next[key] = value
	}
	if err := s.validateFieldValues(next); err != nil {
		return err
	}
	if s.spec.Validate != nil {
		return s.spec.Validate(next)
	}
	return nil
}

func (s *Service) validateFieldValues(values map[string]any) error {
	for _, field := range s.spec.Fields {
		value, ok := values[field.Key]
		if !ok {
			continue
		}
		if err := validateFieldValue(field, value); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldValue(field Descriptor, value any) error {
	switch field.Type {
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("setting %q must be a boolean", field.Key)
		}
	case "integer":
		number, err := exactInteger(value)
		if err != nil {
			return fmt.Errorf("setting %q must be an integer", field.Key)
		}
		if field.Minimum != nil && number < *field.Minimum {
			return fmt.Errorf("setting %q must be at least %v", field.Key, *field.Minimum)
		}
		if field.Maximum != nil && number > *field.Maximum {
			return fmt.Errorf("setting %q must be at most %v", field.Key, *field.Maximum)
		}
	case "string", "duration":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("setting %q must be a string", field.Key)
		}
		if len(field.Enum) > 0 {
			matched := false
			for _, allowed := range field.Enum {
				if text == allowed {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("setting %q has an unsupported value", field.Key)
			}
		}
	default:
		return fmt.Errorf("setting %q has unsupported type %q", field.Key, field.Type)
	}
	return nil
}

const maxExactJSONInteger = float64(1<<53 - 1)

func exactInteger(value any) (float64, error) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.ParseInt(typed.String(), 10, 64)
		if err != nil || parsed > 1<<53-1 || parsed < -(1<<53-1) {
			return 0, fmt.Errorf("integer is outside the exact JSON range")
		}
		return float64(parsed), nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed || math.Abs(typed) > maxExactJSONInteger {
			return 0, fmt.Errorf("integer is outside the exact JSON range")
		}
		return typed, nil
	default:
		number, ok := Number(value)
		if !ok || math.Trunc(number) != number || math.Abs(number) > maxExactJSONInteger {
			return 0, fmt.Errorf("integer is outside the exact JSON range")
		}
		return number, nil
	}
}

func (s *Service) storeCache(doc Document) {
	s.mu.Lock()
	s.cache = &doc
	s.cacheAt = time.Now()
	s.mu.Unlock()
}

func (s *Service) hasField(key string) bool { _, ok := s.field(key); return ok }
func (s *Service) field(key string) (Descriptor, bool) {
	for _, f := range s.spec.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Descriptor{}, false
}
func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneDocument(in Document) Document {
	in.Values = cloneMap(in.Values)
	in.Sources = cloneSources(in.Sources)
	in.Fields = append([]Descriptor(nil), in.Fields...)
	return in
}
func cloneSources(in map[string]Source) map[string]Source {
	out := make(map[string]Source, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func Number(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		parsed, err := strconv.ParseFloat(v.String(), 64)
		return parsed, err == nil
	}
	return 0, false
}
func String(value any) string { v, _ := value.(string); return v }
func Bool(value any) bool     { v, _ := value.(bool); return v }
