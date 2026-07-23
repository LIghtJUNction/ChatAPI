package settings

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

type fakeDomain struct{}

func (fakeDomain) Domain() string                    { return "access" }
func (fakeDomain) Title() string                     { return "Access" }
func (fakeDomain) Fields() []settingscore.Descriptor { return nil }
func (fakeDomain) Get(context.Context) (settingscore.Document, error) {
	return settingscore.Document{Domain: "access"}, nil
}

func TestPatchReportsAfterUpdateFailureWithoutHidingAppliedSettings(t *testing.T) {
	wantErr := errors.New("reconcile failed")
	service := New(config.Config{}, Domain{
		Settings:    fakeDomain{},
		AfterUpdate: func(context.Context, []string) error { return wantErr },
	})
	result, err := service.Patch(context.Background(), "access", PatchInput{Values: map[string]any{"user_conversation_limit": 10}})
	if err != nil {
		t.Fatalf("patch returned error after settings were applied: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], wantErr.Error()) {
		t.Fatalf("patch warnings = %v, want reconciliation failure", result.Warnings)
	}
}
func (fakeDomain) Reload(ctx context.Context) (settingscore.Document, error) {
	return fakeDomain{}.Get(ctx)
}
func (fakeDomain) ValidatePatch(context.Context, map[string]any) error { return nil }
func (fakeDomain) Patch(context.Context, map[string]any) (settingscore.Document, []string, error) {
	return settingscore.Document{Domain: "access"}, nil, nil
}

type validatingDomain struct {
	domain   string
	key      string
	validate error
	patches  int
}

func (d *validatingDomain) Domain() string { return d.domain }
func (d *validatingDomain) Title() string  { return d.domain }
func (d *validatingDomain) Fields() []settingscore.Descriptor {
	return []settingscore.Descriptor{{Key: d.key}}
}
func (d *validatingDomain) Get(context.Context) (settingscore.Document, error) {
	return settingscore.Document{Domain: d.domain}, nil
}
func (d *validatingDomain) Reload(ctx context.Context) (settingscore.Document, error) {
	return d.Get(ctx)
}
func (d *validatingDomain) ValidatePatch(context.Context, map[string]any) error { return d.validate }
func (d *validatingDomain) Patch(context.Context, map[string]any) (settingscore.Document, []string, error) {
	d.patches++
	return settingscore.Document{Domain: d.domain}, nil, nil
}

func TestCombinedPatchValidatesEveryChildBeforeWriting(t *testing.T) {
	access := &validatingDomain{domain: "access-core", key: "user_conversation_limit"}
	realtime := &validatingDomain{domain: "realtime", key: "max_connections", validate: errors.New("invalid realtime setting")}
	combined, err := Combine("access", "Access", access, realtime)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := combined.Patch(context.Background(), map[string]any{
		"user_conversation_limit": 10,
		"max_connections":         -1,
	}); !errors.Is(err, realtime.validate) {
		t.Fatalf("combined validation error=%v", err)
	}
	if access.patches != 0 || realtime.patches != 0 {
		t.Fatalf("patches after validation failure: access=%d realtime=%d", access.patches, realtime.patches)
	}
}

func TestPatchPassesSortedChangedKeysToAfterUpdate(t *testing.T) {
	var changed []string
	service := New(config.Config{}, Domain{
		Settings: fakeDomain{},
		AfterUpdate: func(_ context.Context, keys []string) error {
			changed = append([]string(nil), keys...)
			return nil
		},
	})
	_, err := service.Patch(context.Background(), "access", PatchInput{Values: map[string]any{"z": 1, "a": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"a", "z"}) {
		t.Fatalf("unexpected changed keys: %v", changed)
	}
}
