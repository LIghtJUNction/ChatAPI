package service

import (
	"testing"

	"github.com/zyf/chatapi/internal/config"
)

func TestBuildAdminStorageSchema(t *testing.T) {
	schema := BuildAdminStorageSchema(config.Config{
		DatabaseDriver:                        "sqlite",
		StorageCleanupKeepRecentConversations: 100,
		StorageCleanupKeepRecentDays:          30,
	})
	if len(schema.Operations) < 4 {
		t.Fatalf("unexpected admin storage schema operations: %#v", schema)
	}
	if schema.Operations[0].Name != "set_user_quota" {
		t.Fatalf("unexpected first admin storage operation: %#v", schema.Operations[0])
	}
	foundVacuum := false
	for _, item := range schema.Operations {
		if item.Name == "vacuum" {
			foundVacuum = true
			if len(item.Fields) != 1 || item.Fields[0].Key != "dry_run" {
				t.Fatalf("unexpected vacuum field schema: %#v", item)
			}
		}
	}
	if !foundVacuum {
		t.Fatalf("expected vacuum operation in schema: %#v", schema)
	}
}
