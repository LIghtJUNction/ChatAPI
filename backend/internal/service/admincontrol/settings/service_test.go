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
