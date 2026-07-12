package settingscore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type memoryStore struct {
	item   *common.SystemConfig
	getErr error
}

type blockingStore struct {
	mu          sync.Mutex
	item        common.SystemConfig
	readStarted chan struct{}
	releaseRead chan struct{}
	blockOnce   sync.Once
}

func (s *blockingStore) GetSystemConfig(context.Context, string) (common.SystemConfig, error) {
	s.mu.Lock()
	item := s.item
	s.mu.Unlock()
	blocked := false
	s.blockOnce.Do(func() {
		blocked = true
		close(s.readStarted)
	})
	if blocked {
		<-s.releaseRead
	}
	return item, nil
}

func (s *blockingStore) SetSystemConfig(_ context.Context, input common.SetSystemConfigInput) (common.SystemConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.item.Value = cloneMap(input.Value)
	s.item.UpdatedAt = time.Now().UTC()
	return s.item, nil
}

func (m *memoryStore) GetSystemConfig(context.Context, string) (common.SystemConfig, error) {
	if m.getErr != nil {
		return common.SystemConfig{}, m.getErr
	}
	if m.item == nil {
		return common.SystemConfig{}, common.ErrNotFound
	}
	return *m.item, nil
}
func (m *memoryStore) SetSystemConfig(_ context.Context, input common.SetSystemConfigInput) (common.SystemConfig, error) {
	now := time.Now().UTC()
	createdAt := now
	if m.item != nil {
		createdAt = m.item.CreatedAt
	}
	m.item = &common.SystemConfig{Key: input.Key, Value: cloneMap(input.Value), CreatedAt: createdAt, UpdatedAt: now}
	return *m.item, nil
}

func TestServicePatchUsesLastWriteWinsAndEnvironmentAuthority(t *testing.T) {
	store := &memoryStore{}
	minimum := float64(0)
	service := New(store, Spec{Domain: "media", StorageKey: "settings.media", Defaults: map[string]any{"max": 10, "locked": false}, Environment: map[string]any{"locked": true}, Fields: []Descriptor{{Key: "max", Type: "integer", Editable: true, Minimum: &minimum}, {Key: "locked", Type: "boolean", Editable: true}}, Validate: func(values map[string]any) error {
		value, _ := Number(values["max"])
		if value < 0 {
			return errors.New("negative")
		}
		return nil
	}})
	doc, err := service.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Sources["locked"] != SourceEnvironment || !Bool(doc.Values["locked"]) {
		t.Fatalf("unexpected initial document: %#v", doc)
	}
	_, _, err = service.Patch(context.Background(), map[string]any{"max": 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Patch(context.Background(), map[string]any{"max": 30}); err != nil {
		t.Fatalf("last write was rejected: %v", err)
	}
	if _, _, err := service.Patch(context.Background(), map[string]any{"locked": false}); err == nil {
		t.Fatal("expected environment setting rejection")
	}
	if _, _, err := service.Patch(context.Background(), map[string]any{"max": -1}); err == nil {
		t.Fatal("expected validation rejection")
	}
	current, _ := service.Get(context.Background())
	if value, _ := Number(current.Values["max"]); value != 30 {
		t.Fatalf("validation failure changed snapshot: %#v", current.Values)
	}
}

func TestServiceReloadsExpiredSnapshotFromSharedStore(t *testing.T) {
	store := &memoryStore{}
	service := New(store, Spec{Domain: "chat", StorageKey: "settings.chat", Defaults: map[string]any{"value": 1}, Fields: []Descriptor{{Key: "value", Type: "integer", Editable: true}}})
	if _, err := service.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetSystemConfig(context.Background(), common.SetSystemConfigInput{Key: "settings.chat", Value: map[string]any{"value": 2}}); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.cacheAt = time.Now().Add(-3 * time.Second)
	service.mu.Unlock()
	next, err := service.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	value, _ := Number(next.Values["value"])
	if value != 2 {
		t.Fatalf("stale value: %#v", next.Values)
	}
}

func TestPatchMergesAgainstCurrentPersistedDocument(t *testing.T) {
	store := &memoryStore{}
	service := New(store, Spec{Domain: "chat", StorageKey: "settings.chat", Defaults: map[string]any{"value": 1}, Fields: []Descriptor{{Key: "value", Type: "integer", Editable: true}}})
	if _, _, err := service.Patch(context.Background(), map[string]any{"value": 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetSystemConfig(context.Background(), common.SetSystemConfigInput{Key: "settings.chat", Value: map[string]any{"value": 3, "future": true}}); err != nil {
		t.Fatal(err)
	}
	_, _, err := service.Patch(context.Background(), map[string]any{"value": 4})
	if err != nil {
		t.Fatalf("last write failed: %v", err)
	}
	if store.item.Value["future"] != true {
		t.Fatalf("current persisted fields were lost: %#v", store.item.Value)
	}
}

func TestServiceReturnsLastKnownGoodSnapshotWhenRefreshFails(t *testing.T) {
	store := &memoryStore{}
	service := New(store, Spec{
		Domain:     "chat",
		StorageKey: "settings.chat",
		Defaults:   map[string]any{"value": 1},
		Fields:     []Descriptor{{Key: "value", Type: "integer", Editable: true}},
	})
	_, _, err := service.Patch(context.Background(), map[string]any{"value": 2})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.cacheAt = time.Now().Add(-3 * time.Second)
	service.mu.Unlock()
	store.getErr = errors.New("database unavailable")

	fallback, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("expected last-known-good snapshot, got %v", err)
	}
	if !fallback.Stale {
		t.Fatalf("unexpected fallback: %#v", fallback)
	}
	if fallback.RefreshError == "" {
		t.Fatal("expected refresh error metadata")
	}
	value, _ := Number(fallback.Values["value"])
	if value != 2 {
		t.Fatalf("fallback value=%v", value)
	}
}

func TestPatchPreservesUnknownAndEnvironmentCoveredPersistedValues(t *testing.T) {
	now := time.Now().UTC()
	store := &memoryStore{item: &common.SystemConfig{
		Key:       "settings.media",
		Value:     map[string]any{"max": float64(10), "locked": false, "future_field": "keep-me"},
		CreatedAt: now,
		UpdatedAt: now,
	}}
	service := New(store, Spec{
		Domain:      "media",
		StorageKey:  "settings.media",
		Defaults:    map[string]any{"max": 1, "locked": false},
		Environment: map[string]any{"locked": true},
		Fields: []Descriptor{
			{Key: "max", Type: "integer", Editable: true},
			{Key: "locked", Type: "boolean", Editable: true},
		},
	})
	if _, _, err := service.Patch(context.Background(), map[string]any{"max": float64(20)}); err != nil {
		t.Fatal(err)
	}
	if store.item.Value["future_field"] != "keep-me" {
		t.Fatalf("unknown field was lost: %#v", store.item.Value)
	}
	if value, ok := store.item.Value["locked"].(bool); !ok || value {
		t.Fatalf("covered persisted value was lost: %#v", store.item.Value)
	}
	doc, err := service.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !Bool(doc.Values["locked"]) {
		t.Fatalf("environment value is not effective: %#v", doc.Values)
	}
}

func TestPatchRejectsValuesThatDoNotMatchDescriptorTypes(t *testing.T) {
	service := New(&memoryStore{}, Spec{
		Domain:     "test",
		StorageKey: "settings.test",
		Defaults:   map[string]any{"enabled": true, "limit": 1},
		Fields: []Descriptor{
			{Key: "enabled", Type: "boolean", Editable: true},
			{Key: "limit", Type: "integer", Editable: true},
		},
	})
	if _, _, err := service.Patch(context.Background(), map[string]any{"enabled": "true"}); err == nil {
		t.Fatal("expected boolean string to be rejected")
	}
	if _, _, err := service.Patch(context.Background(), map[string]any{"limit": 1.5}); err == nil {
		t.Fatal("expected fractional integer to be rejected")
	}
	if _, _, err := service.Patch(context.Background(), map[string]any{"limit": json.Number("9007199254740992")}); err == nil {
		t.Fatal("expected unsafe JSON integer to be rejected")
	}
}

func TestPatchCannotBeOverwrittenByOlderConcurrentReload(t *testing.T) {
	store := &blockingStore{
		item:        common.SystemConfig{Key: "settings.chat", Value: map[string]any{"value": 1}},
		readStarted: make(chan struct{}),
		releaseRead: make(chan struct{}),
	}
	service := New(store, Spec{
		Domain:     "chat",
		StorageKey: "settings.chat",
		Defaults:   map[string]any{"value": 0},
		Fields:     []Descriptor{{Key: "value", Type: "integer", Editable: true}},
	})
	reloadDone := make(chan error, 1)
	go func() {
		_, err := service.Reload(context.Background())
		reloadDone <- err
	}()
	<-store.readStarted
	patchDone := make(chan error, 1)
	go func() {
		_, _, err := service.Patch(context.Background(), map[string]any{"value": 2})
		patchDone <- err
	}()
	select {
	case err := <-patchDone:
		t.Fatalf("patch bypassed in-flight reload: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(store.releaseRead)
	if err := <-reloadDone; err != nil {
		t.Fatal(err)
	}
	if err := <-patchDone; err != nil {
		t.Fatal(err)
	}
	doc, err := service.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	value, _ := Number(doc.Values["value"])
	if value != 2 {
		t.Fatalf("older reload replaced patched cache: %#v", doc.Values)
	}
}
