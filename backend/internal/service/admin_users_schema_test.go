package service

import "testing"

func TestBuildAdminUsersSchema(t *testing.T) {
	schema := BuildAdminUsersSchema()
	if len(schema.Operations) != 5 {
		t.Fatalf("unexpected admin users schema operations: %#v", schema)
	}
	if schema.Operations[1].Name != "create_user" || len(schema.Operations[1].Fields) != 4 {
		t.Fatalf("unexpected admin users create schema: %#v", schema.Operations[1])
	}
	if schema.Operations[3].Name != "reset_user_password" {
		t.Fatalf("unexpected admin users reset password schema: %#v", schema.Operations[3])
	}
}
