package settings

import (
	"context"
	"reflect"
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
func (fakeDomain) Reload(ctx context.Context) (settingscore.Document, error) {
	return fakeDomain{}.Get(ctx)
}
func (fakeDomain) Patch(context.Context, map[string]any) (settingscore.Document, []string, error) {
	return settingscore.Document{Domain: "access"}, nil, nil
}

func TestPatchPassesSortedChangedKeysToAfterUpdate(t *testing.T) {
	var changed []string
	service := New(config.Config{}, Domain{
		Settings: fakeDomain{},
		AfterUpdate: func(_ context.Context, keys []string) {
			changed = append([]string(nil), keys...)
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
