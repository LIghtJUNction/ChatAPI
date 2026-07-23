package conversationretention

import (
	"context"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type fakeUsers struct{ items []common.User }

func (f fakeUsers) ListUsers(context.Context) ([]common.User, error) { return f.items, nil }

type recordingPruner struct{ calls []string }

func (p *recordingPruner) PruneConversations(_ context.Context, ownerID string, keep int) (common.DeleteConversationsResult, int, error) {
	p.calls = append(p.calls, ownerID)
	return common.DeleteConversationsResult{}, keep, nil
}

func TestSettingsUpdatedOnlyReconcilesConversationLimit(t *testing.T) {
	pruner := &recordingPruner{}
	service := New(fakeUsers{items: []common.User{{ID: "a"}, {ID: "b"}}}, pruner, func(context.Context) int { return 30 }, nil)

	service.SettingsUpdated(context.Background(), []string{"global_rate_limit_requests"})
	if len(pruner.calls) != 0 {
		t.Fatalf("unrelated setting triggered reconciliation: %v", pruner.calls)
	}
	service.SettingsUpdated(context.Background(), []string{LimitSettingKey})
	if len(pruner.calls) != 2 || pruner.calls[0] != "a" || pruner.calls[1] != "b" {
		t.Fatalf("unexpected reconciliation calls: %v", pruner.calls)
	}
}

func TestEnforceSkipsUnlimitedPolicy(t *testing.T) {
	pruner := &recordingPruner{}
	service := New(nil, pruner, func(context.Context) int { return 0 }, nil)
	service.Enforce(context.Background(), "a")
	if len(pruner.calls) != 0 {
		t.Fatalf("unlimited policy triggered prune: %v", pruner.calls)
	}
}
