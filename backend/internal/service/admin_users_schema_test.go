package service

import "testing"

func TestBuildAdminUsersSchema(t *testing.T) {
	schema := BuildAdminUsersSchema()
	if len(schema.Operations) != 12 {
		t.Fatalf("unexpected admin users schema operations: %#v", schema)
	}
	if schema.Operations[1].Name != "create_user" || len(schema.Operations[1].Fields) != 4 {
		t.Fatalf("unexpected admin users create schema: %#v", schema.Operations[1])
	}
	if schema.Operations[3].Name != "list_user_identities" {
		t.Fatalf("unexpected admin users list identities schema: %#v", schema.Operations[3])
	}
	if schema.Operations[4].Name != "preview_user_purge" {
		t.Fatalf("unexpected admin users preview purge schema: %#v", schema.Operations[4])
	}
	if schema.Operations[5].Name != "transfer_user_ownership" || len(schema.Operations[5].Fields) != 1 {
		t.Fatalf("unexpected admin users ownership transfer schema: %#v", schema.Operations[5])
	}
	if schema.Operations[6].Name != "list_user_ownership_items" {
		t.Fatalf("unexpected admin users ownership item schema: %#v", schema.Operations[6])
	}
	if schema.Operations[7].Name != "transfer_user_ownership_selection" || len(schema.Operations[7].Fields) != 3 {
		t.Fatalf("unexpected admin users ownership selection schema: %#v", schema.Operations[7])
	}
	if schema.Operations[9].Name != "unlink_user_identity" {
		t.Fatalf("unexpected admin users unlink identity schema: %#v", schema.Operations[9])
	}
	if schema.Operations[10].Name != "deactivate_user" {
		t.Fatalf("unexpected admin users deactivate schema: %#v", schema.Operations[10])
	}
	if schema.Operations[11].Name != "purge_user" {
		t.Fatalf("unexpected admin users purge schema: %#v", schema.Operations[11])
	}
}
